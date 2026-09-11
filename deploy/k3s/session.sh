#!/bin/sh
# Manual reference for the allocator's Job -> UID -> owned Service transaction.
set -eu

namespace=vif
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
	exit 2
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
		kube -n "$namespace" get jobs,pods,services \
			-l app.kubernetes.io/part-of=vi-fighter-fleet -o wide
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
