package main

import "strconv"

type workloadConfig struct {
	Namespace string
	Image     string
	// BridgeImage is the WebSocket sidecar. Empty is a fleet that publishes no
	// browser route, and then the pod is exactly what it was before one existed.
	BridgeImage string
	Players     int
	LogLevel    string
	MapSize     string
	Scenario    string
	FirstJoin   string
	Empty       string
	Drain       string
}

// bridgeSidecar serves the browser route from inside the pod: one WebSocket in,
// one loopback connection to the game out, binary frames because the protocol is
// bytes. A substitute image must accept this vector; see doc/kube-docker-deploy.md.
//
// It is a restartable init container rather than an ordinary one, which is what
// keeps a bridge fault from ending a match: a Job pod whose ordinary container
// exits non-zero is Failed, and backoffLimit 0 makes that the end of the session.
func bridgeSidecar(image string, security map[string]any) map[string]any {
	return map[string]any{
		"name":            "ws-bridge",
		"image":           image,
		"imagePullPolicy": "IfNotPresent",
		"restartPolicy":   "Always",
		"args": []string{"--binary", "--exit-on-eof",
			"ws-l:0.0.0.0:" + wsBridgePort, "tcp:127.0.0.1:7777"},
		"ports": []any{
			map[string]any{"name": "wsgame", "containerPort": 7779, "protocol": "TCP"},
		},
		"securityContext": security,
		"resources": map[string]any{
			"requests": map[string]string{"cpu": "25m", "memory": "16Mi"},
			"limits":   map[string]string{"cpu": "100m", "memory": "32Mi"},
		},
	}
}

// wadCategories are the resource directories a session reads from the node's
// volume. content/ is not among them and will not be: glyphs are player domain,
// read on each player's own machine. audio/ is not yet — a dedicated host renders
// nothing — and adding it back is adding it here, in update-vif-wad.sh, and in
// 30-session.yaml, which is the whole of provisioning a category to the fleet.
var wadCategories = []string{"scenario", "image"}

// wadMounts is shared by the init container and the session, which must prove the
// same tree. subPath rather than the whole root, so a category the node happens to
// hold is not silently in a pod that was never meant to read it.
func wadMounts() []any {
	mounts := make([]any, 0, len(wadCategories))
	for _, name := range wadCategories {
		mounts = append(mounts, map[string]any{"name": "fleet-wad",
			"mountPath": "/wad/" + name, "subPath": name, "readOnly": true})
	}
	return mounts
}

func buildJob(id string, cfg workloadConfig) map[string]any {
	labels := sessionLabels(id)
	containerSecurity := map[string]any{
		"allowPrivilegeEscalation": false,
		"privileged":               false,
		"readOnlyRootFilesystem":   true,
		"runAsNonRoot":             true,
		"runAsUser":                65532,
		"capabilities": map[string]any{
			"drop": []string{"ALL"},
		},
		"seccompProfile": map[string]any{"type": "RuntimeDefault"},
	}

	// The check runs to completion first; the bridge follows it as a sidecar, so
	// it is already serving when the game binds its own port.
	initContainers := []any{
		map[string]any{
			"name":            "config-check",
			"image":           cfg.Image,
			"imagePullPolicy": "IfNotPresent",
			"args":            []string{"-check", "-config-dir", "/wad", "-s", cfg.Scenario},
			"securityContext": containerSecurity,
			"volumeMounts":    wadMounts(),
			"resources": map[string]any{
				"requests": map[string]string{"cpu": "50m", "memory": "64Mi"},
				"limits":   map[string]string{"cpu": "500m", "memory": "192Mi"},
			},
		},
	}
	if cfg.BridgeImage != "" {
		initContainers = append(initContainers, bridgeSidecar(cfg.BridgeImage, containerSecurity))
	}

	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      sessionPrefix + id,
			"namespace": cfg.Namespace,
			"labels":    labels,
		},
		"spec": map[string]any{
			"backoffLimit":            0,
			"completions":             1,
			"parallelism":             1,
			"activeDeadlineSeconds":   14400,
			"ttlSecondsAfterFinished": 120,
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec": map[string]any{
					"restartPolicy":                 "Never",
					"serviceAccountName":            "vif-session",
					"automountServiceAccountToken":  false,
					"enableServiceLinks":            false,
					"terminationGracePeriodSeconds": 30,
					"securityContext": map[string]any{
						"runAsNonRoot": true,
						"runAsUser":    65532,
						"runAsGroup":   65532,
						"fsGroup":      65532,
						"seccompProfile": map[string]any{
							"type": "RuntimeDefault",
						},
					},
					"initContainers": initContainers,
					"containers": []any{
						map[string]any{
							"name":            "session",
							"image":           cfg.Image,
							"imagePullPolicy": "IfNotPresent",
							"args": []string{
								"-serve", ":7777",
								"-probe", ":7778",
								"-authority", "host",
								"-l=/var/log/vif-fleet",
								"-log-session-id=" + id,
								"-lv", cfg.LogLevel,
								"-ls", "all+dispatch",
								"-config-dir", "/wad",
								"-s", cfg.Scenario,
								"-size", cfg.MapSize,
								"-players", strconv.Itoa(cfg.Players),
								"-first-join", cfg.FirstJoin,
								"-empty", cfg.Empty,
								"-drain", cfg.Drain,
							},
							"ports": []any{
								map[string]any{"name": "game", "containerPort": 7777, "protocol": "TCP"},
								map[string]any{"name": "probe", "containerPort": 7778, "protocol": "TCP"},
							},
							// Deployment state, never request state: a caller chooses
							// from what limits advertises and nothing else. Optional,
							// so an absent map is not a pod that will not start.
							"envFrom": []any{
								map[string]any{"configMapRef": map[string]any{
									"name": "vif-session-env", "optional": true}},
							},
							// After envFrom, so the fleet's own envelope wins.
							"env": []any{
								map[string]any{"name": "GOMEMLIMIT", "value": "160MiB"},
							},
							"securityContext": containerSecurity,
							"volumeMounts": append([]any{
								map[string]any{
									"name":      "fleet-logs",
									"mountPath": "/var/log/vif-fleet",
								},
							}, wadMounts()...),
							"resources": map[string]any{
								"requests": map[string]string{"cpu": "100m", "memory": "96Mi"},
								"limits":   map[string]string{"cpu": "500m", "memory": "192Mi"},
							},
							"startupProbe": map[string]any{
								"httpGet":          map[string]any{"path": "/health", "port": "probe"},
								"periodSeconds":    2,
								"failureThreshold": 30,
							},
							"livenessProbe": map[string]any{
								"httpGet":          map[string]any{"path": "/health", "port": "probe"},
								"periodSeconds":    5,
								"timeoutSeconds":   2,
								"failureThreshold": 3,
							},
						},
					},
					"volumes": []any{
						map[string]any{
							"name": "fleet-logs",
							"persistentVolumeClaim": map[string]any{
								"claimName": "vif-fleet-logs",
							},
						},
						map[string]any{
							"name": "fleet-wad",
							"persistentVolumeClaim": map[string]any{
								"claimName": "vif-fleet-wad",
								"readOnly":  true,
							},
						},
					},
				},
			},
		},
	}
}

func buildService(id, jobUID string, nodePort int, namespace string) map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": map[string]any{
			"name":      sessionPrefix + id,
			"namespace": namespace,
			"labels":    sessionLabels(id),
			"ownerReferences": []any{
				map[string]any{
					"apiVersion":         "batch/v1",
					"kind":               "Job",
					"name":               sessionPrefix + id,
					"uid":                jobUID,
					"controller":         true,
					"blockOwnerDeletion": true,
				},
			},
		},
		"spec": map[string]any{
			"type": "NodePort",
			"selector": map[string]string{
				labelSession: id,
			},
			"externalTrafficPolicy": "Local",
			"ports": []any{
				map[string]any{
					"name":       "game",
					"port":       7777,
					"targetPort": "game",
					"nodePort":   nodePort,
					"protocol":   "TCP",
				},
			},
		},
	}
}
