package main

import "strconv"

type workloadConfig struct {
	Namespace string
	Image     string
	Players   int
	MapSize   string
	FirstJoin string
	Empty     string
	Drain     string
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
					"initContainers": []any{
						map[string]any{
							"name":            "config-check",
							"image":           cfg.Image,
							"imagePullPolicy": "IfNotPresent",
							"args":            []string{"-check", "-d"},
							"securityContext": containerSecurity,
							"resources": map[string]any{
								"requests": map[string]string{"cpu": "50m", "memory": "64Mi"},
								"limits":   map[string]string{"cpu": "500m", "memory": "192Mi"},
							},
						},
					},
					"containers": []any{
						map[string]any{
							"name":            "session",
							"image":           cfg.Image,
							"imagePullPolicy": "IfNotPresent",
							"args": []string{
								"-serve", ":7777",
								"-probe", ":7778",
								"-authority", "host",
								"-log-stdout",
								"-lv", "info",
								"-ls", "all+dispatch",
								"-d",
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
							"env": []any{
								map[string]any{"name": "GOMEMLIMIT", "value": "160MiB"},
							},
							"securityContext": containerSecurity,
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
