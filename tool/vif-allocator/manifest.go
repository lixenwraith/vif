package main

import "strconv"

// Each bot grows the session container by a seat's measured cost at 120x40, about
// 28 MB resident and 60 ms of CPU a second (doc/kubernetes-fleet.md §6).
const (
	botMemoryMi = 32
	botCPUMilli = 64
	// A session's memory limit before bots: td's 500x250 map peaks near 215 MiB
	// with one guest. GOMEMLIMIT sits 32 MiB under it.
	sessionMemoryMi = 480
	// The most bots whose limit fits the LimitRange's 672Mi container ceiling.
	sessionBotsMax = (672 - sessionMemoryMi) / botMemoryMi
)

type workloadConfig struct {
	Namespace string
	Image     string
	Players   int
	Bots      int
	LogLevel  string
	Size      string // the host's -size, or sizeFirst
	Scenario  string
	FirstJoin string
	Empty     string
	Drain     string
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
	args := []string{
		"-serve", ":7777",
		"-probe", ":7778",
		// The name both routes carry, so neither can seat a
		// player whose link named another session.
		"-name", id,
		"-authority", "host",
		"-l=/var/log/vif-fleet",
		"-log-session-id=" + id,
		"-lv", cfg.LogLevel,
		"-ls", "all+dispatch",
		"-config-dir", "/wad",
		"-s", cfg.Scenario,
		"-players", strconv.Itoa(cfg.Players),
		"-bots", strconv.Itoa(cfg.Bots),
		"-first-join", cfg.FirstJoin,
		"-empty", cfg.Empty,
		"-drain", cfg.Drain,
	}
	// Without -size the host takes its first guest's terminal (adoptLobbyGeometry).
	if cfg.Size != sizeFirst {
		args = append(args, "-size", cfg.Size)
	}
	perBot := func(base, step int) string { return strconv.Itoa(base + step*cfg.Bots) }
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

	// The check runs to completion before the game starts.
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

	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      sessionPrefix + id,
			"namespace": cfg.Namespace,
			"labels":    labels,
			"annotations": map[string]string{
				annotationScenario: cfg.Scenario,
				annotationSize:     cfg.Size,
			},
		},
		"spec": map[string]any{
			"backoffLimit":            0,
			"completions":             1,
			"parallelism":             1,
			"activeDeadlineSeconds":   3600,
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
							"args":            args,
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
								map[string]any{"name": "GOMEMLIMIT", "value": perBot(sessionMemoryMi-32, botMemoryMi) + "MiB"},
							},
							"securityContext": containerSecurity,
							"volumeMounts": append([]any{
								map[string]any{
									"name":      "fleet-logs",
									"mountPath": "/var/log/vif-fleet",
								},
							}, wadMounts()...),
							"resources": map[string]any{
								"requests": map[string]string{
									"cpu": perBot(100, botCPUMilli) + "m", "memory": perBot(96, botMemoryMi) + "Mi"},
								"limits": map[string]string{"cpu": "500m", "memory": perBot(sessionMemoryMi, botMemoryMi) + "Mi"},
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
