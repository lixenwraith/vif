# Generated from cmd/vif/usage.go by TestGeneratedFilesAreTheHelpTable; do not edit.
_vif() {
	local cur=${COMP_WORDS[COMP_CWORD]}
	COMPREPLY=()
	case ${COMP_WORDS[COMP_CWORD-1]} in
	-host) ;;
	-serve) ;;
	-cert) ;;
	-key) ;;
	-ca) ;;
	-join) ;;
	-name) ;;
	-players) ;;
	-authority) mapfile -t COMPREPLY < <(compgen -W 'host migrate' -- "$cur") ;;
	-slow-window) ;;
	-slow-late) ;;
	-slow-bytes) ;;
	-listen) ;;
	-relay) ;;
	-size) ;;
	-probe) ;;
	-first-join) ;;
	-empty) ;;
	-drain) ;;
	-config-dir) compopt -o dirnames ;;
	-s|-config-scenario) compopt -o default ;;
	-f|-config-content) compopt -o default ;;
	-k|-config-keymap) compopt -o default ;;
	-config-music) compopt -o default ;;
	-config-sounds) compopt -o default ;;
	-color) mapfile -t COMPREPLY < <(compgen -W 'auto 256 true' -- "$cur") ;;
	-scale) ;;
	-ab|-audio-backend) ;;
	-seed) ;;
	-speed) ;;
	-script) compopt -o default ;;
	-r|-replay) compopt -o default ;;
	-lv|-log-level) ;;
	-ls|-log-scope) ;;
	-lt|-log-stat) ;;
	-lr|-log-recorder) ;;
	-log-session-id) ;;
	*) mapfile -t COMPREPLY < <(compgen -W '-host -serve -cert -key -selfsign -ca -join -name -players -bot -authority -slow-window -slow-late -slow-bytes -listen -no-advertise -relay -size -probe -first-join -empty -drain -d -config-embedded -config-dir -s -config-scenario -f -config-content -k -config-keymap -config-music -config-sounds -color -g -gui -cli -maximized -fullscreen -scale -mute -ab -audio-backend -nw -no-wait -seed -speed -script -headless -watch -r -replay -check -schema -l -log -lv -log-level -ls -log-scope -lt -log-stat -lr -log-recorder -log-session-id -log-stdout -j -journal -mw -music-wav -dev -h -help -version' -- "$cur") ;;
	esac
}
complete -F _vif vif
