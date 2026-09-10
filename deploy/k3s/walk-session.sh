#!/usr/bin/env bash
# walk-session.sh: walk one live session outward from the pod and record what each boundary answers.
# Usage: [CAPTURE=seconds] ./walk-session.sh SESSION_ID [NODE_ADDR]; run on the node as the operator.
set -uo pipefail

[[ $# -ge 1 ]] || { echo "usage: $0 SESSION_ID [NODE_ADDR]" >&2; exit 2; }
sid=$1
node=${2:-$(ip -4 route get 1.1.1.1 | awk '{ for (i = 1; i < NF; i++) if ($i == "src") { print $(i + 1); exit } }')}
svc=vif-session-$sid
sel=vif.lixenwraith.dev/session=$sid
kc=(sudo kubectl -n vif)

sec() { printf '\n==================== %s ====================\n' "$*"; }

# offer dials host:port and prints the bytes the coordinator sends unprompted within two seconds.
offer() {
	local out rc
	out=$(timeout 6 bash -c 'exec 3<>"/dev/tcp/$0/$1" && timeout 2 head -c 64 <&3 | od -An -tx1 | tr -s " \n" " "' "$1" "$2" 2>&1)
	rc=$?
	printf '%-24s rc=%-3s %s\n' "$1:$2" "$rc" "${out//$'\n'/ }"
}

sudo -v || exit 1
date -u +%FT%TZ

sec "objects"
"${kc[@]}" get job,pod,svc -l "$sel" -o wide
"${kc[@]}" get endpointslice -l "kubernetes.io/service-name=$svc" \
	-o jsonpath='{range .items[*].endpoints[*]}{.addresses[0]} {.conditions}{"\n"}{end}'
"${kc[@]}" get pod -l "$sel" \
	-o jsonpath='{range .items[*].status.containerStatuses[*]}{.name} {.imageID} restarts={.restartCount}{"\n"}{end}'
pod_ip=$("${kc[@]}" get pod -l "$sel" -o jsonpath='{.items[0].status.podIP}' 2>/dev/null)
cluster_ip=$("${kc[@]}" get svc "$svc" -o jsonpath='{.spec.clusterIP}' 2>/dev/null)
nodeport=$("${kc[@]}" get svc "$svc" -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null)
printf 'pod=%s clusterIP=%s nodePort=%s node=%s\n' "${pod_ip:-?}" "${cluster_ip:-?}" "${nodeport:-?}" "${node:-?}"
[[ -n $pod_ip && -n $cluster_ip && -n $nodeport && -n $node ]] || { echo "session $sid is incomplete" >&2; exit 1; }

sec "walk, pod outward"
offer "$pod_ip" 7777
offer "$cluster_ip" 7777
offer 127.0.0.1 "$nodeport"
offer "$node" "$nodeport"

sec "health from a node-local source"
curl -sS -m 3 "http://$pod_ip:7778/health"; echo
curl -sS -m 3 "http://$pod_ip:7778/metrics" | head -n 5

sec "pod-log envelope, key sets over the last 200 lines"
"${kc[@]}" logs "job/$svc" --tail=200 | python3 -c '
import collections, json, sys
sets: collections.Counter[tuple[str, ...]] = collections.Counter()
bad = 0
for line in sys.stdin:
    try:
        sets[tuple(sorted(json.loads(line)))] += 1
    except (TypeError, ValueError):
        bad += 1
for keys, n in sets.most_common():
    print(n, list(keys))
print("unparsed:", bad)
'

if (( ${CAPTURE:-0} > 0 )); then
	sec "SYNs to 7777 at cni0 from outside the pod network for ${CAPTURE}s: probe from the host and off-box now"
	sudo timeout "$CAPTURE" tcpdump -lnni cni0 -c 8 'tcp dst port 7777 and tcp[tcpflags] & tcp-syn != 0 and not src net 10.42.0.0/16' 2>&1
	sec "conntrack for NodePort $nodeport (reply dst must equal orig src: no SNAT)"
	sudo conntrack -L -p tcp --orig-port-dst "$nodeport" 2>&1
fi

sec "session log tail"
"${kc[@]}" logs "job/$svc" --tail=15
