#!/bin/sh
# Manual reference for the allocator's Job -> UID -> owned Service transaction.
set -eu

namespace=vif
fleet_label=app.kubernetes.io/part-of=vi-fighter-fleet
fleet_logs=${VIF_FLEET_LOGS:-/var/log/vif-fleet}
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

kube() {
	if [ -n "${KUBECTL:-}" ]; then
		"$KUBECTL" "$@"
	else
		sudo kubectl "$@"
	fi
}

usage() {
	echo "usage: $0 create SESSION_ID [GAME_NODEPORT] [IMAGE] [PLAYERS] [MAP_SIZE]" >&2
	echo "       $0 delete SESSION_ID" >&2
	echo "       $0 list" >&2
	echo "       $0 status" >&2
	echo "       $0 blockers" >&2
	echo "       $0 drain [--force]" >&2
	exit 2
}

fleet_objects() {
	kube -n "$namespace" get jobs,pods,services -l "$fleet_label" -o name
}

# One verdict per fleet object, because "these names exist" does not tell an
# operator whether anyone is playing. A finished Job keeps its object for
# ttlSecondsAfterFinished after the match ended, which is why the public session
# list can be empty while an update still refuses.
fleet_report() {
	kube -n "$namespace" get jobs,pods,services -l "$fleet_label" -o json |
		jq -r --argjson now "$(date -u +%s)" '
			def since($t): (($now - ($t | fromdateiso8601)) | floor);
			.items[] |
			.metadata.name as $name |
			if .kind == "Job" then
				(.spec.ttlSecondsAfterFinished // 0) as $ttl |
				(.status.completionTime //
					([.status.conditions[]? |
						select((.type == "Complete" or .type == "Failed") and
							((.status | ascii_downcase) == "true")) |
						.lastTransitionTime] | first)) as $ended |
				if .metadata.deletionTimestamp then
					"job/\($name): deleting for \(since(.metadata.deletionTimestamp))s, waiting on its pod"
				elif $ended then
					"job/\($name): finished \(since($ended))s ago, nobody is playing; " +
					(($ttl - since($ended)) as $left |
						if $left > 0 then "its TTL removes it in \($left)s"
						else "its TTL expired and the controller is behind" end)
				else
					"job/\($name): active with \(.status.active // 0) pod(s), a match may be in play"
				end
			elif .kind == "Pod" then
				"pod/\($name): \(.status.phase // "Unknown")" +
				(if .metadata.deletionTimestamp
					then ", terminating for \(since(.metadata.deletionTimestamp))s"
					else ", held by its Job" end)
			else
				"service/\($name): NodePort \(.spec.ports[0].nodePort // 0)"
			end'
}

session_name() {
	case "$1" in
		'' | *[!a-z0-9-]*) echo "$0: invalid SESSION_ID: $1" >&2; exit 2 ;;
	esac
	printf 'vif-session-%s\n' "$1"
}

free_port() {
	used=$(kube get services --all-namespaces \
		-o jsonpath='{range .items[*].spec.ports[*]}{.nodePort}{"\n"}{end}')
	for port in 31700 31701 31702 31703 31704 31705 31706 31707 31708 31709; do
		if ! printf '%s\n' "$used" | grep -qx "$port"; then
			printf '%s\n' "$port"
			return
		fi
	done
	echo "$0: all fleet NodePorts are allocated" >&2
	exit 1
}

split_manifest() {
	manifest=$1
	job=$2
	service=$3
	awk -v job="$job" -v service="$service" '
		/^---$/ { document++; next }
		document == 0 { print > job; next }
		document == 1 { print > service }
	' "$manifest"
}

command=${1:-}
case "$command" in
	list)
		[ "$#" -eq 1 ] || usage
		kube -n "$namespace" get jobs,pods,services -l "$fleet_label" -o wide
		;;
	status)
		[ "$#" -eq 1 ] || usage
		echo '# fleet objects'
		report=$(fleet_report)
		printf '%s\n' "${report:-the fleet is empty}"
		echo '# fleet log files'
		if [ -d "$fleet_logs" ]; then
			sudo find "$fleet_logs" -mindepth 1 -maxdepth 1 -print
		fi
		echo '# units'
		systemctl is-active 'var-log-vif\x2dfleet.mount' k3s.service \
			vif-allocator.service logwisp.service \
			vif-fleet-log-cleanup.timer || true
		;;
	blockers)
		# The assertion the update helpers make before they touch anything.
		[ "$#" -eq 1 ] || usage
		report=$(fleet_report)
		if [ -z "$report" ]; then
			echo 'the fleet is empty'
			exit 0
		fi
		printf '%s\n' "$report" >&2
		echo "$0: the fleet is not empty" >&2
		echo "$0: \`$0 drain\` deletes these objects and the retained session log files" >&2
		echo "$0: \`$0 drain --force\` also abandons a pod that will not terminate" >&2
		exit 1
		;;
	drain)
		# The update helpers refuse a non-empty fleet, and the LogWisp gate also
		# requires an empty tmpfs. This is destructive: snapshot any log evidence
		# before running it.
		force=false
		case $# in
			1) ;;
			2) [ "$2" = --force ] || usage; force=true ;;
			*) usage ;;
		esac
		for object in $(kube -n "$namespace" get jobs,services -l "$fleet_label" -o name); do
			kube -n "$namespace" delete "$object" \
				--cascade=background --wait=false --ignore-not-found
		done
		remaining=$(fleet_objects)
		attempt=0
		while [ -n "$remaining" ] && [ "$attempt" -lt 60 ]; do
			sleep 1
			attempt=$((attempt + 1))
			remaining=$(fleet_objects)
		done
		if [ -n "$remaining" ] && [ "$force" = true ]; then
			# The owning Jobs are already deleted, so nothing replaces what this
			# abandons; the container may outlive the object until the kubelet
			# reaps it, and its NodePort frees only then.
			echo 'the graceful cascade did not finish; abandoning the surviving pods'
			for object in $(kube -n "$namespace" get pods -l "$fleet_label" -o name); do
				kube -n "$namespace" delete "$object" \
					--force --grace-period=0 --ignore-not-found
			done
			remaining=$(fleet_objects)
			attempt=0
			while [ -n "$remaining" ] && [ "$attempt" -lt 30 ]; do
				sleep 1
				attempt=$((attempt + 1))
				remaining=$(fleet_objects)
			done
		fi
		if [ -n "$remaining" ]; then
			echo "$0: the fleet did not drain:" >&2
			fleet_report >&2
			if [ "$force" = false ]; then
				echo "$0: rerun as \`$0 drain --force\` to abandon the surviving pods" >&2
			fi
			exit 1
		fi
		if [ -d "$fleet_logs" ]; then
			sudo find "$fleet_logs" -mindepth 1 -maxdepth 1 -name '*.jsonl' -delete
			leftover=$(sudo find "$fleet_logs" -mindepth 1 -maxdepth 1 -print)
			if [ -n "$leftover" ]; then
				echo "$0: unexpected entries under $fleet_logs:" >&2
				printf '%s\n' "$leftover" >&2
				exit 1
			fi
		fi
		echo 'fleet drained: no objects, no log files'
		;;
	delete)
		[ "$#" -eq 2 ] || usage
		name=$(session_name "$2")
		kube -n "$namespace" delete job "$name" \
			--cascade=background --wait=false --ignore-not-found
		kube -n "$namespace" delete service "$name" --ignore-not-found
		;;
	create)
		[ "$#" -ge 2 ] && [ "$#" -le 6 ] || usage
		id=$2
		name=$(session_name "$id")
		existing=$(kube -n "$namespace" get job,service "$name" \
			--ignore-not-found -o name)
		if [ -n "$existing" ]; then
			printf '%s: %s already exists; run `%s delete %s` first\n' \
				"$0" "$existing" "$0" "$id" >&2
			exit 1
		fi
		port=${3:-$(free_port)}
		image=${4:-${IMAGE:-vi-fighter:dev}}
		players=${5:-${PLAYERS:-4}}
		map_size=${6:-${MAP_SIZE:-120x40}}

		work=$(mktemp -d)
		created_job=false
		cleanup() {
			status=$?
			trap - EXIT HUP INT TERM
			if [ "$status" -ne 0 ] && [ "$created_job" = true ]; then
				kube -n "$namespace" delete job "$name" \
					--cascade=background --wait=false --ignore-not-found >/dev/null 2>&1 || true
			fi
			rm -rf "$work"
			exit "$status"
		}
		trap cleanup EXIT HUP INT TERM

		"$script_dir/render-session.sh" "$id" "$port" "$image" "$players" "$map_size" \
			>"$work/session.yaml"
		split_manifest "$work/session.yaml" "$work/job.yaml" "$work/service.yaml"
		kube create --dry-run=server -f "$work/job.yaml" >/dev/null
		kube create -f "$work/job.yaml"
		created_job=true
		uid=$(kube -n "$namespace" get job "$name" -o jsonpath='{.metadata.uid}')

		JOB_UID=$uid "$script_dir/render-session.sh" \
			"$id" "$port" "$image" "$players" "$map_size" >"$work/session-owned.yaml"
		split_manifest "$work/session-owned.yaml" "$work/job-owned.yaml" "$work/service-owned.yaml"
		kube create --dry-run=server -f "$work/service-owned.yaml" >/dev/null
		kube create -f "$work/service-owned.yaml"
		created_job=false
		printf 'session=%s port=%s image=%s job_uid=%s\n' "$id" "$port" "$image" "$uid"
		;;
	*)
		usage
		;;
esac
