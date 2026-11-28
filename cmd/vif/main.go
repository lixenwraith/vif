package main

import (
	"cmp"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app"
	"github.com/lixenwraith/vif/internal/bot"
	"github.com/lixenwraith/vif/internal/converge"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/lifecycle"
	"github.com/lixenwraith/vif/internal/manifest"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/internal/vlog"
	link "github.com/lixenwraith/vif/pkg/network"
)

// Exit codes
const (
	exitFailure  = 1
	exitUsage    = 2  // a flag refused, as the flag package's own exit
	exitLogSetup = 73 // EX_CANTCREAT: logging requested but unavailable
	exitRefused  = 76 // EX_PROTOCOL: a session refused the join, saying whether a retry helps
)

const logShutdownTimeout = 2 * time.Second

// colorModes are the -color words. Auto is the default and means "ask the
// terminal", which is what every run that does not care wants.
const (
	colorAuto = "auto"
	color256  = "256"
	colorTrue = "true"
)

// CLI flags. helpSections describes every one and flag.Usage prints it, so no
// registration carries a usage string.
var (
	flagColor    = flag.String("color", colorAuto, "")
	flagMute     = flag.Bool("mute", true, "")
	flagNoWait   = flag.Bool("nw", true, "")
	flagCheck    = flag.Bool("check", false, "")
	flagSchema   = flag.Bool("schema", false, "")
	flagSpeed    = flag.String("speed", "", "")
	flagTick     = flag.Duration("tick", parameter.GameUpdateInterval, "")
	flagSeed     = flag.Uint64("seed", 0, "")
	flagGUI      = flag.Bool("g", false, "")
	flagCLI      = flag.Bool("cli", false, "")
	flagMaxim    = flag.Bool("maximized", false, "")
	flagFull     = flag.Bool("fullscreen", false, "")
	flagScale    = flag.Int("scale", 1, "")
	flagReplay   = flag.String("replay", "", "")
	flagScript   = flag.String("script", "", "")
	flagHeadless = flag.Bool("headless", false, "")
	flagGames    = flag.Int("games", 0, "")
	flagField    = flag.Bool("field", false, "")
	flagWatch    = flag.Bool("watch", false, "")
	flagHelp     = flag.Bool("h", false, "")
	flagVer      = flag.Bool("version", false, "")

	flagAudioBackend string
	flagConfig       = newConfigFlags()
	flagLogs         = newLogFlags()
	flagSession      sessionFlags
	flagJournal      = newSetFlag(true, parseOutputDirFlag)
	flagMusicWAV     = newSetFlag(true, parseOutputDirFlag)
	flagDev          = newSetFlag(true, parseBoolFlag)

	// settings is vif.toml as the roots of -config-dir resolved it, brought up to
	// this build's, settingsFile the file it came from ("" the embedded one), and
	// settingsStore where the run writes it back as [play] changes; settingsNotes
	// name what bringing it up to date changes, settingsNotice what writing it and
	// the keymap back said, and noticeSaid whether stdout has said it.
	settings       paths.Settings
	settingsFile   string
	settingsStore  paths.SettingsStore
	settingsNotes  []string
	settingsNotice string
	noticeSaid     bool
)

func init() {
	flagConfig.register(flag.CommandLine)
	flagLogs.register(flag.CommandLine)
	flagSession.register(flag.CommandLine)
	flag.StringVar(&flagAudioBackend, "ab", "", "")
	flag.StringVar(&flagAudioBackend, "audio-backend", "", "")
	flag.BoolVar(flagHelp, "help", false, "")
	flag.BoolVar(flagGUI, "gui", false, "")
	flag.BoolVar(flagNoWait, "no-wait", true, "")
	flag.StringVar(flagReplay, "r", "", "")
	flag.Var(&flagJournal, "j", "")
	flag.Var(&flagJournal, "journal", "")
	flag.Var(&flagMusicWAV, "mw", "")
	flag.Var(&flagMusicWAV, "music-wav", "")
	flag.Var(&flagDev, "dev", "")

	// The `flag` package writes its own usage to stderr and exits non-zero, which
	// is right for a mistake and wrong for a question. Asking is handled in main.
	flag.Usage = func() { writeUsage(flag.CommandLine.Output()) }
}

func main() {
	// Reached only where a build took the parser's own report and exit from it
	if err := flag.CommandLine.Parse(botArgs(flag.CommandLine, os.Args[1:])); err != nil {
		complain(err.Error())
		os.Exit(exitUsage)
	}
	if *flagHelp {
		// Asked for, so it is output rather than a diagnostic: stdout, exit zero,
		// greppable without redirecting stderr.
		writeUsage(os.Stdout)
		return
	}
	if *flagVer {
		writeVersion(os.Stdout)
		return
	}
	var err error
	if settings, settingsFile, settingsNotes, err = paths.LoadSettings(flagConfig.Dir); err != nil {
		complain(err.Error())
		os.Exit(exitFailure)
	}
	settingsStore = paths.SettingsFor(flagConfig.Dir, settingsFile)
	settingsOwn := paths.OwnSettings(flagConfig.Dir, settingsFile)
	if pageSettings != nil {
		settingsStore, settingsNotes = pageSettings(&settings)
		settingsOwn = true
	}
	// -g given either way decides, and -cli over it; unset, vif.toml's
	// [presentation] or the build does. The window's state follows the same rule.
	window := windowByDefault || settings.Presentation.Window
	windowMaximized, windowFullscreen = settings.Presentation.Maximized, settings.Presentation.Fullscreen
	cli, asked := false, "-g" // asked: what asked for the window, named when the build has none
	if settings.Presentation.Window {
		asked = cmp.Or(settingsFile, paths.SettingsFile) + ": presentation.window"
	}
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "g", "gui":
			window, asked = *flagGUI, "-g"
		case "cli":
			cli = *flagCLI
		case "maximized":
			windowMaximized = *flagMaxim
		case "fullscreen":
			windowFullscreen = *flagFull
		}
	})
	if cli {
		window, asked = false, "-cli"
	} else if !window {
		asked = "-g=false"
	}
	switch {
	case window && openWindow == nil:
		complain(asked + ": desktop window unavailable in this build")
		os.Exit(exitFailure)
	case window:
		openWindow()
	case windowOnly:
		complain(asked + ": terminal unavailable in this build")
		os.Exit(exitFailure)
	}
	requested := flagConfig.Scenario // what a -join site is asked for; vif.toml's is this machine's
	applySettings(settings, settingsOwn)

	setupDiagnostics()

	sessionErr := validateInvocation(*flagSchema, *flagCheck, *flagReplay, *flagScript, *flagWatch, flagSession)
	bots := flagSession.botCount()
	if sessionErr == nil && *flagHeadless && (*flagWatch || bots == 0 && *flagReplay == "" || flagSession.serve != "") {
		sessionErr = errors.New("-headless requires -bot or -replay and cannot combine with -watch or -serve")
	}
	if sessionErr == nil && (*flagGames < 0 || *flagGames != 0 && !(*flagHeadless && bots > 0)) {
		sessionErr = errors.New("-games is how many games a -headless -bot run learns from at once, 1 or more")
	}
	if sessionErr == nil && *flagField && !((*flagWatch || *flagHeadless) && bots > 0) {
		sessionErr = errors.New("-field is how a -watch or -headless -bot run plays what its bot learned, teaching it nothing")
	}
	if sessionErr == nil && flagSession.cert != "" {
		sessionTLS, sessionErr = link.LoadTLS(flagSession.cert, flagSession.key)
	}
	if sessionErr == nil && flagSession.selfsign {
		if sessionTLS, sessionErr = link.SelfSignedTLS(flagSession.doorHosts()); sessionErr == nil {
			fmt.Printf("-selfsign: a wss:// door encrypts with a certificate made for this run, %s; a terminal joins it as it is, or pins it with -ca\n",
				link.Fingerprint(sessionTLS))
		}
	}
	if sessionErr == nil && flagSession.ca != "" {
		sessionTrust, sessionErr = link.TrustTLS(flagSession.ca)
	}
	if sessionErr == nil && flagSession.passphrase == "new" {
		if flagSession.join != "" || flagSession.spectate != "" {
			sessionErr = errors.New("-passphrase new makes a host's passphrase; a joiner gives the host's")
		} else {
			flagSession.passphrase = network.GeneratePassphrase()
			fmt.Printf("-passphrase: the doors take %s; :session shows it, and a joiner gives it with -passphrase or :join <target> <passphrase>\n", flagSession.passphrase)
		}
	}
	if sessionErr == nil && flagSession.passphrase != "" {
		sessionErr = network.CheckPassphrase(flagSession.passphrase)
	}
	if sessionErr == nil && flagSession.admit != "" && flagSession.admit != "open" {
		if spec, ok := strings.CutPrefix(flagSession.admit, "ticket:"); ok {
			sessionTickets, sessionErr = network.ParseTicketKeys(spec)
		} else {
			sessionErr = fmt.Errorf("-admit %q: open, or ticket:<key>", flagSession.admit)
		}
	}
	if sessionErr == nil {
		sessionErr = requestSiteSession(requested)
	}
	if sessionErr == nil { // a refused invocation writes nothing
		if settingsNotice = upgradeOwn(settingsOwn && len(settingsNotes) > 0); settingsNotice != "" {
			vlog.Info("app", "msg", "settings upgraded", "notice", settingsNotice)
		}
	}
	switch {
	case sessionErr != nil:
		err = sessionErr
	case *flagSchema:
		err = manifest.Schema(os.Stdout)
	case *flagCheck:
		switch {
		case len(settingsNotes) == 0:
			fmt.Println("settings ok:", cmp.Or(settingsFile, "embedded default"))
		case settingsOwn:
			fmt.Printf("settings %s: %s: %s\n", settingsFile, paths.WriteBack(settingsFile), strings.Join(settingsNotes, "; "))
		default:
			fmt.Printf("settings %s: a run reads it up to date: %s\n", settingsFile, strings.Join(settingsNotes, "; "))
		}
		err = resource.Check(buildConfig().Resources, os.Stdout)
	case flagSession.botFiles() != nil:
		err = botFiles(buildConfig().Resources, flagSession.botFiles())
	case *flagReplay != "" && *flagHeadless:
		sayNotice()
		err = verifyJournal(*flagReplay)
	case *flagReplay != "":
		err = app.PlayJournal(buildConfig(), *flagReplay)
	case *flagScript != "":
		cfg, host := buildConfig(), platformHost
		if *flagWatch {
			cfg.Mode, host = app.ModeScript, presenting()
		} else {
			sayNotice()
		}
		_, err = app.RunScript(cfg, host, *flagScript)
	case (*flagWatch || *flagHeadless) && bots > 0:
		cfg, host := buildConfig(), platformHost
		graph := cfg.Membership.Bots[0]
		cfg.Membership.Bots = cfg.Membership.Bots[1:]
		if *flagWatch {
			cfg.Mode, host = app.ModeScript, presenting()
		} else {
			sayNotice()
			botNotice(cfg, graph)
			if *flagField {
				cfg.Round = func(r bot.Round) { fmt.Println(r) }
			}
		}
		var st bot.Stats
		if st, err = app.RunBot(cfg, host, graph); err == nil && !*flagWatch {
			games := ""
			if st.Games > 1 {
				games = fmt.Sprintf("%d games, ", st.Games)
			}
			fmt.Printf("Bot stopped: %s%d ticks, %d actions, %d rounds, score %.0f\n", games, st.Ticks, st.Injected, st.Rounds, st.Score)
			for _, l := range st.Learn {
				switch {
				case l.Scored > 0:
					fmt.Printf("Bot learned %s: rounds %d of %d, recent %.0f, fielding %.0f over %d\n",
						l.Network, l.Scored, l.Rounds, l.Recent, l.Fielded, l.FieldedRounds)
				case l.Kept:
					fmt.Printf("Bot fielded %s: rounds %d, recent %.0f, fielding %.0f over %d\n",
						l.Network, l.Rounds, l.Recent, l.Fielded, l.FieldedRounds)
				}
			}
		}
	case flagSession.serve != "":
		sayNotice()
		err = app.RunServer(buildConfig())
	default:
		err = app.Run(buildConfig(), presenting())
	}
	if speakersOff != nil {
		speakersOff()
	}

	shutdownDiagnostics()

	if err != nil {
		if settingsNotice != "" && !noticeSaid {
			fmt.Fprintln(os.Stderr, settingsNotice) // the run may have ended before showing it
		}
		complain(err.Error())
		os.Exit(exitCode(err))
	}
}

// botFiles runs a -bot verb that keeps bot files for the scenario asked, and
// prints what it says
func botFiles(o resource.Options, words []string) error {
	sc, err := resource.LoadScenario(o)
	if err != nil {
		return err
	}
	lines, err := resource.BotFiles(o, sc, words)
	for _, line := range lines {
		fmt.Println(line)
	}
	return err
}

// exitCode is a returned error's code. A refused join has its own: its reason
// already says whether a retry helps, so a launcher shows it without offering one.
func exitCode(err error) int {
	var refused *network.Refusal
	if errors.As(err, &refused) {
		return exitRefused
	}
	return exitFailure
}

// verifyJournal reports what a headless replay reproduced; leaving the run is the error.
func verifyJournal(path string) error {
	st, err := app.VerifyJournal(buildConfig(), path)
	if err != nil {
		return err
	}
	fmt.Printf("Replay verified to run %d tick %d: %d records, %d worlds written, %d digests reproduced\n",
		st.End.Run, st.End.Tick, st.Injected, st.Installed, st.Digests)
	return nil
}

// upgradeOwn writes the player's own vif.toml back brought up to this build's when
// settings says it is not, and their own keymap so, once, unless the run only reads
// them (-check, -schema); it returns the status line naming the changes.
func upgradeOwn(settings bool) string {
	if *flagCheck || *flagSchema {
		return ""
	}
	notice := ""
	if settings {
		notice = paths.UpgradeStore(settingsStore)
	}
	return app.JoinNotice(notice, resource.UpgradeKeymap(flagConfig.Options))
}

// sayNotice prints what the start changed for a run that draws nothing, which has
// no status line to open with it
func sayNotice() {
	if settingsNotice != "" {
		fmt.Println(settingsNotice)
		noticeSaid = true
	}
}

// botNotice says what a headless bot run is doing, since it draws nothing: solo it
// plays until its graph quits, in as many games as it learns from at once, and
// hosting it waits in the lobby for players.
func botNotice(cfg app.Config, spec string) {
	name, games, err := app.BotRun(cfg, spec)
	// A joining run's bot is named by the session's scenario once it is in
	who := strings.TrimSpace("Bot " + cmp.Or(name, spec))
	switch {
	case err != nil: // RunBot refuses the graph or the count, and says so
	case cfg.Membership.HostAddress != "":
		fmt.Printf("%s hosting %s (headless); Ctrl-C stops it\n", who, cfg.Membership.HostAddress)
	case cfg.Membership.JoinAddress != "":
		fmt.Printf("%s joining %s (headless); Ctrl-C stops it\n", who, cfg.Membership.JoinAddress)
	case games > 1:
		fmt.Printf("%s running headless in %d games; Ctrl-C stops it\n", who, games)
	default:
		fmt.Printf("%s running headless; Ctrl-C stops it\n", who)
	}
}

// requestSiteSession replaces a -join naming a site with the link of a session that
// site creates for this player, before anything is built from the flags.
func requestSiteSession(scenario string) error {
	site, err := link.ParseEndpoint(flagSession.join)
	if err != nil || site.Scheme != link.SchemeSite {
		return nil
	}
	fmt.Printf("requesting a session from %s\n", site.Addr)
	var signIn string
	if paths.ExternalFiles() {
		signIn = app.SignInFor(paths.PlaceFile{Path: paths.SignInsFile()}, site.Addr)
	}
	// The session's own bots are the menu's to ask for: -bot here seats this player's
	target, fingerprint, err := network.RequestSession(site.Addr, flagSession.players, 0, scenario, signIn)
	if err != nil {
		return fmt.Errorf("-join %s: %w", site.Addr, err)
	}
	// A front door the router made its own certificate for is pinned by the
	// fingerprint the site handed out with the link, as -ca would pin it.
	if fingerprint != "" && sessionTrust == nil {
		if sessionTrust, err = link.TrustTLS(fingerprint); err != nil {
			return fmt.Errorf("-join %s: the door's fingerprint %q: %w", site.Addr, fingerprint, err)
		}
	}
	fmt.Printf("joining %s, the link others join by\n", target)
	flagSession.join, flagSession.players = target, 0
	return nil
}

// setupDiagnostics installs the crash hook and session defaults, starts a log
// session if any log flag was given and runtime capture if enabled, before the
// terminal enters the alternate screen. A log session asked for that cannot start
// is fatal here: reported at exit, a supervised process would have played a whole
// session unlogged.
func setupDiagnostics() {
	core.SetCrashHook(vlog.CrashHook)
	vlog.SetCrashFlush(status.CrashFlush) // drains while the sink is still live

	logDir := flagLogs.dir.value
	if logDir == "" {
		logDir = paths.DefaultLogDir()
	}
	journalDir := flagJournal.value
	if journalDir == "" {
		journalDir = paths.DefaultJournalDir()
	}
	vlog.Configure(vlog.Config{
		Dir:        logDir,
		JournalDir: journalDir,
		Level:      flagLogs.level.value,
		Scope:      flagLogs.scope.value,
		SessionID:  flagLogs.session.value,
		Console:    flagLogs.console,
		Spawn:      core.Go, // processor panics reach HandleCrash, terminal restored
	})

	// -l and -j are boolean flags, so the space form leaves a path unparsed.
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "ignoring arguments %v; use -l=DIR or -j=DIR\n", flag.Args())
	}

	if flagLogs.enabled() {
		path, err := vlog.Start()
		if err != nil {
			// Fatal rather than degraded: logging was asked for, and a run that
			// cannot write it has no way to say so afterwards.
			fmt.Fprintf(os.Stderr, "-l: %v\n", err)
			os.Exit(exitLogSetup)
		}
		fmt.Printf("logging enabled: %s (level %s, scope %s)\n",
			path, vlog.LevelName(), vlog.ScopeString(vlog.Scopes()))
	}

	// -schema runs nothing to capture, and a tool reads its stdout as JSON
	if flagDev.valueOr(core.RaceEnabled) && !*flagSchema {
		path, err := core.CaptureStderr(logDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "runtime capture disabled: %v\n", err)
			return
		}
		reason := "-dev"
		if !flagDev.set {
			reason = "race build"
		}
		fmt.Printf("runtime capture: %s (%s)\n", path, reason)
		vlog.Info("app", "msg", "runtime capture started",
			"path", path, "reason", reason, "race", core.RaceEnabled)
		core.StartStderrDrain(parameter.DevDrainInterval, logRuntimeReport)
	}

}

// shutdownDiagnostics drains what the runtime wrote, closes the logger, then
// hands fd 2 back so late runtime output reaches the restored terminal
// Journal paths go to stdout like the launch notices: fd 2 still feeds the
// capture file here, and a path line would also keep an empty capture on disk
func shutdownDiagnostics() {
	core.StopStderrDrain()
	core.DrainStderr(logRuntimeReport) // last blocks, while the sink lives

	vlog.Shutdown(logShutdownTimeout)

	for _, path := range vlog.JournalPaths() {
		fmt.Printf("replay journal: %s\n", path)
	}
	if path := core.CloseCapture(); path != "" && core.CaptureCount() > 0 {
		fmt.Fprintf(os.Stderr, "runtime output captured: %s (%d report(s))\n",
			path, core.CaptureCount())
	}
}

// logRuntimeReport records a pointer to one captured block
// Error level so the scope mask never filters a race or fatal report
func logRuntimeReport(r core.RuntimeReport) {
	vlog.Error("race", "msg", "runtime report",
		"kind", r.Kind,
		"path", r.Path,
		"offset", r.Offset,
		"bytes", r.Bytes,
		"lines", r.Lines,
		"head", r.Head,
		"at", r.At)
	status.Trigger(status.TrigRace)
}

// platformHost is the build's platform host, empty on the terminal: openWindow makes
// it the desktop window, nil without one, which windowByDefault opens unless -g=false
// and windowOnly marks a build with no terminal to play in; it opens maximized or full
// screen as asked. A browser, with no vif.toml, keeps its settings, its runs' places
// and its bots' learning in pageSettings, pagePlaces and pagePopulations, and shows
// a scenario link's files as they arrive by pageProgress.
var (
	platformHost                      app.Host
	openWindow                        func()
	windowByDefault, windowOnly       bool
	windowMaximized, windowFullscreen bool
	pageSettings                      func(*paths.Settings) (paths.SettingsStore, []string)
	pagePlaces                        app.PlaceStore
	pagePopulations                   resource.Populations
	pageProgress                      resource.Progress
	sessionTLS, sessionTrust          *tls.Config // -cert and -key or -selfsign, and -ca, read once validated
	sessionTickets                    [][]byte    // the router's public keys -admit ticket names
)

// complain says why the run ends before or after its screen: on stderr, or in a
// box where a build has no console to say it on
var complain = func(msg string) { fmt.Fprintln(os.Stderr, msg) }

// speakersOn gives a presenting run the build's own speakers; speakersOff closes them
var speakersOn, speakersOff func()

// presenting is the platform host of a run that presents: the build's own speakers
// open only here, so a run that draws nothing holds no stream
func presenting() app.Host {
	if speakersOn != nil {
		speakersOn()
	}
	return platformHost
}

// buildConfig translates parsed flags into the runtime configuration
func buildConfig() app.Config {
	cfg := app.Config{
		AudioBackend:  flagAudioBackend,
		Resources:     flagConfig.Options,
		LogScope:      flagLogs.scope.value,
		StatTicks:     flagLogs.stat.value,
		RecTicks:      flagLogs.rec.value,
		TimeScaleSpec: *flagSpeed,
		TickInterval:  *flagTick,
		Seed:          *flagSeed,
		Games:         *flagGames,
		Field:         *flagField,
		Journal:       flagJournal.set,
		Membership: app.Membership{
			SessionName:  flagSession.name,
			Participants: flagSession.players,
		},
	}
	cfg.Resources.Populations = pagePopulations
	cfg.Resources.Progress = pageProgress

	// Validated in validateInvocation; a link's name overrides nothing, because a
	// joiner has no -name of its own. A spectator dials as a joiner does.
	watched, follow, _ := flagSession.spectated()
	if target := cmp.Or(flagSession.join, watched); target != "" {
		join := endpoint(target)
		cfg.Membership.JoinAddress = join.Dialable()
		cfg.Membership.SessionName = cmp.Or(join.Name, cfg.Membership.SessionName)
		cfg.Membership.Watch, cfg.Membership.Follow = watched != "", follow
	}
	cfg.Membership.Spectators, _ = flagSession.audience()
	cfg.Membership.HostAddress = doorList(flagSession.host)
	cfg.Membership.ListenAddress = endpoint(flagSession.listen).Addr
	cfg.Membership.RelayAddress = relayTarget(flagSession.relay)
	cfg.Membership.TLS, cfg.Membership.Trust = sessionTLS, sessionTrust
	cfg.Membership.Passphrase = flagSession.passphrase
	cfg.Membership.TicketKeys = sessionTickets

	if flagSession.serve != "" {
		cfg.Membership.HostAddress = doorList(flagSession.serve)
		cfg.ProbeAddress = flagSession.probe
		cfg.Lifetime = flagSession.lifetime()
	}
	// A session ends with its host unless the host asks for it to move: migrating
	// shares the address of each guest that may take it over, which nobody is
	// signed up for by a default.
	cfg.Membership.Migrate = flagSession.authority == authorityMigrate
	cfg.Membership.NoCheats = flagSession.noCheats
	cfg.Membership.SlowPolicy = &flagSession.slow
	cfg.Membership.ShedPolicy = &flagSession.shed
	if cfg.FitSize = flagSession.size == sizeFit; flagSession.size != "" && !cfg.FitSize {
		cfg.Width, cfg.Height, _ = parseSize(flagSession.size) // validated in validateInvocation
	}
	cfg.Membership.Bots, _ = app.BotSpecs(flagSession.botSpec())

	cfg.Play, cfg.Settings, cfg.Notice = settings.Play, settingsStore, settingsNotice
	// A page keeps its places for its tab; a terminal or a window keeps them past the
	// process in the user's state directory while [session] resume_after_restart says.
	// A dedicated host has no place to come back to, and a pod's filesystem is read-only.
	cfg.Membership.PlaceStore = pagePlaces
	if pagePlaces == nil && paths.ExternalFiles() {
		cfg.Membership.SignInStore = paths.PlaceFile{Path: paths.SignInsFile()}
	}
	if pagePlaces == nil && settings.Session.ResumeAfterRestart && flagSession.serve == "" {
		cfg.Membership.PlaceStore = paths.PlaceFile{Path: paths.PlacesFile()}
	}
	// -mute either way names both channels for this run, and -nw the wait; unset,
	// [play] stands, and a value set here is never written back
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "mute":
			cfg.Play.Sfx, cfg.Play.Music = !*flagMute, !*flagMute
		case "nw", "no-wait":
			cfg.Play.Wait = !*flagNoWait
		}
	})
	cfg.MusicWAV = musicWAVDir()
	cfg.AudioBuffer = time.Duration(settings.Audio.BufferMs) * time.Millisecond
	cfg.NoTmuxFrameCap = !settings.Presentation.TmuxFrameCap

	switch *flagColor {
	case colorTrue:
		cfg.ColorMode, cfg.ColorModeSet = terminal.ColorModeTrueColor, true
	case color256:
		cfg.ColorMode, cfg.ColorModeSet = terminal.ColorMode256, true
	}
	// colorAuto leaves ColorModeSet false, which is the terminal deciding.

	return cfg
}

// musicWAVDir is where -mw records the music, "" when it was not asked for.
func musicWAVDir() string {
	if !flagMusicWAV.set {
		return ""
	}
	return cmp.Or(flagMusicWAV.value, paths.DefaultMusicDir())
}

// applySettings makes vif.toml the default of each path flag it names
// (resource.Options.ApplySettings, own false for a vif.toml not the player's own): a
// flag given on the command line keeps its value, and -l and -j still decide
// whether a stream is written.
func applySettings(s paths.Settings, own bool) {
	flagConfig.ApplySettings(s, own)
	p := s.Paths
	flagLogs.dir.value = cmp.Or(flagLogs.dir.value, p.Log)
	flagJournal.value = cmp.Or(flagJournal.value, p.Journal)
	flagMusicWAV.value = cmp.Or(flagMusicWAV.value, p.Music)
}

// configFlags groups every runtime file override. Short flags remain for
// compatibility; config-* aliases make the family discoverable in CLI help.
type configFlags struct{ resource.Options }

func newConfigFlags() *configFlags { return &configFlags{} }

func (f *configFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.Dir, "config-dir", "", "")
	fs.StringVar(&f.Music, "config-music", "", "")
	fs.StringVar(&f.Sounds, "config-sounds", "", "")

	for _, alias := range []struct {
		short, long string
		into        *string
	}{
		{"s", "config-scenario", &f.Scenario},
		{"f", "config-content", &f.Content},
		{"k", "config-keymap", &f.Keymap},
	} {
		fs.StringVar(alias.into, alias.short, "", "")
		fs.StringVar(alias.into, alias.long, "", "")
	}

	fs.BoolVar(&f.Embedded, "d", false, "")
	fs.BoolVar(&f.Embedded, "config-embedded", false, "")
}

// sessionFlags expose startup hosting/joining and the cap a later :host inherits.
type sessionFlags struct {
	host       string
	join       string
	serve      string
	probe      string
	size       string
	players    int
	bot        string
	authority  string
	noCheats   bool
	listen     string
	relay      string
	cert, key  string
	selfsign   bool
	ca         string
	passphrase string
	admit      string

	// spectate is the session a run only watches and, after a space, the slot it
	// looks from first, as :spectate takes them; spectators is how many a hosted
	// session feeds at once, a count kept as given so that none given is the default.
	spectate   string
	spectators string

	// slow is the eviction policy a host applies to a participant that cannot keep
	// up, and shed what it drops when it cannot keep its own tick; see
	// converge.SlowPolicy and converge.ShedPolicy.
	slow converge.SlowPolicy
	shed converge.ShedPolicy

	// name is what a host answers to when one address serves several sessions. A
	// joiner carries it in the -join target rather than here, because a player is
	// given one link and not two things to type.
	name string

	// firstJoin, empty and drain bound an allocated session's life. They are zero
	// on an interactively started host, which is supervised by the person who
	// started it, and set by a deployment whose sessions are created on a player's
	// behalf and have nobody to notice that nobody came.
	firstJoin time.Duration
	empty     time.Duration
	drain     time.Duration
}

// lifetime is the policy these flags describe.
func (f sessionFlags) lifetime() lifecycle.Policy {
	return lifecycle.Policy{FirstJoin: f.firstJoin, Empty: f.empty, Drain: f.drain}
}

func (f *sessionFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.host, "host", "", "")
	fs.StringVar(&f.join, "join", "", "")
	fs.StringVar(&f.spectate, "spectate", "", "")
	fs.StringVar(&f.spectators, "spectators", "", "")
	fs.StringVar(&f.name, "name", "", "")
	fs.StringVar(&f.serve, "serve", "", "")
	fs.StringVar(&f.cert, "cert", "", "")
	fs.StringVar(&f.key, "key", "", "")
	fs.BoolVar(&f.selfsign, "selfsign", false, "")
	fs.StringVar(&f.ca, "ca", "", "")
	fs.StringVar(&f.passphrase, "passphrase", "", "")
	fs.StringVar(&f.admit, "admit", "", "")
	fs.StringVar(&f.probe, "probe", "", "")
	fs.StringVar(&f.size, "size", "", "")
	fs.DurationVar(&f.firstJoin, "first-join", 0, "")
	fs.DurationVar(&f.empty, "empty", 0, "")
	fs.DurationVar(&f.drain, "drain", 0, "")
	fs.IntVar(&f.players, "players", 0, "")
	fs.StringVar(&f.bot, "bot", "", "")
	fs.StringVar(&f.listen, "listen", "", "")
	fs.StringVar(&f.relay, "relay", "", "")
	fs.DurationVar(&f.slow.Window, "slow-window", parameter.NetworkSlowWindow, "")
	fs.Float64Var(&f.slow.LatePerSecond, "slow-late", parameter.NetworkSlowLatePerSecond, "")
	fs.Float64Var(&f.slow.BytesPerSecond, "slow-bytes", parameter.NetworkSlowBytesPerSecond, "")
	f.shed.Share = parameter.NetworkShedShare
	fs.DurationVar(&f.shed.Window, "shed-window", parameter.NetworkShedWindow, "")
	fs.StringVar(&f.authority, "authority", "", "")
	fs.BoolVar(&f.noCheats, "no-cheats", false, "")
}

// authorityMigrate and authorityHost are the two -authority words, the ones
// :host takes.
const (
	authorityMigrate = app.AuthorityMigrate
	authorityHost    = app.AuthorityHost
)

func (f sessionFlags) validateInvocation(schema, check bool, replay string) error {
	if f.authority != "" && f.authority != authorityMigrate && f.authority != authorityHost {
		return fmt.Errorf("-authority %q is not %q or %q", f.authority, authorityMigrate, authorityHost)
	}
	if f.authority != "" && f.host == "" && f.serve == "" {
		return fmt.Errorf("-authority is the policy a host sets for its session; a guest adopts the one it is offered")
	}
	if (f.host != "" || f.join != "" || f.serve != "" || f.probe != "" || f.players != 0 || f.bot != "" ||
		f.authority != "" || f.listen != "" || f.relay != "" || f.name != "" || f.cert != "" || f.key != "" ||
		f.selfsign || f.ca != "" || f.spectate != "" || f.spectators != "" || f.lifetime().Bounded()) && (schema || check || replay != "") {
		return fmt.Errorf("-host, -join, -spectate, -serve, -probe, -players, -spectators, -bot, -authority, -listen, -relay, -name, -cert, -key, -selfsign, -ca and the session lifetime bounds are available only in interactive play")
	}
	if f.spectate != "" && (f.host != "" || f.join != "" || f.serve != "") {
		return fmt.Errorf("-spectate only watches a session; it does not combine with -host, -join or -serve")
	}
	if _, _, err := f.spectated(); err != nil {
		return err
	}
	if _, err := f.audience(); err != nil {
		return err
	}
	if f.key != "" && f.cert == "" {
		return fmt.Errorf("-key is the key of a -cert certificate")
	}
	if f.selfsign && f.cert != "" {
		return fmt.Errorf("-selfsign makes a certificate for this run; -cert names one")
	}
	if f.ca != "" && f.join == "" && f.spectate == "" {
		return fmt.Errorf("-ca is what a joiner trusts a wss:// host by; it takes a -join or a -spectate")
	}
	switch verb, rest := f.botVerb(); {
	case verb == "drop":
		return errors.New("-bot drop takes a bot out of a run: :bot drop <slot> there")
	case verb == "add" && len(rest) > 1:
		return errors.New("usage: -bot " + engine.BotUsage(verb))
	case f.botFiles() != nil && (f.host != "" || f.join != "" || f.serve != "" || f.spectate != ""):
		return fmt.Errorf("-bot %s keeps bot files and exits; it takes no run", verb)
	}
	if _, err := app.BotSpecs(f.botSpec()); err != nil {
		return err
	}
	if e, err := link.ParseEndpoint(f.join); f.players != 0 && f.join != "" && err == nil && e.Scheme != link.SchemeSite {
		return fmt.Errorf("-players configures a host, or the session a -join site creates")
	}
	if f.name != "" && (f.join != "" || (f.host == "" && f.serve == "")) {
		return fmt.Errorf("-name is what a host answers to; a joiner names the session in its -join target")
	}
	for _, a := range []struct{ flag, target string }{{"-host", f.host}, {"-serve", f.serve}} {
		doors, err := link.Doors(a.target)
		for _, d := range doors {
			if d.Secure() && f.cert == "" && !f.selfsign {
				err = errors.New("a wss:// door serves a certificate; give one with -cert or make one with -selfsign")
			}
		}
		if a.target != "" && err != nil {
			return fmt.Errorf("%s: %w", a.flag, err)
		}
	}
	watched, _, _ := f.spectated()
	for _, a := range []struct {
		flag, target string
		binds        bool
	}{{"-join", f.join, false}, {"-spectate", watched, false}, {"-listen", f.listen, true}, {"-relay", f.relay, true}} {
		if a.target == "" {
			continue
		}
		e, err := link.ParseEndpoint(a.target)
		// -relay vif://<router> publishes the door through the router rather than
		// binding one, so it is a dial's shape, named by the router.
		router := a.flag == "-relay" && strings.HasPrefix(a.target, "vif://")
		switch {
		case err != nil:
		case a.flag == "-spectate" && e.Scheme == link.SchemeSite:
			err = errors.New("a site makes sessions to join; spectate one by its own link")
		case a.binds && e.Name != "":
			err = errors.New("a bound address names no session; a host answers to -name")
		case a.binds && e.Scheme == link.SchemeWebSocket:
			err = errors.New("participants link to each other over tcp")
		case router:
		case a.binds:
			err = e.Listenable()
		case e.Name != "":
			err = validSessionName(e.Name)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", a.flag, err)
		}
	}
	if (f.listen != "" || f.relay != "") && f.join == "" {
		return fmt.Errorf("-listen and -relay describe a participant that joined a session; a host already binds one")
	}
	if f.serve != "" && (f.host != "" || f.join != "") {
		return fmt.Errorf("-serve is a host of its own; it does not combine with -host or -join")
	}
	if f.probe != "" && f.serve == "" {
		return fmt.Errorf("-probe answers for a -serve run; nothing else has a supervisor to answer")
	}
	if f.serve == "" && f.lifetime().Bounded() {
		return fmt.Errorf("-first-join, -empty and -drain bound an allocated -serve session; an interactive run is ended by its operator")
	}
	if err := f.lifetime().Validate(); err != nil {
		return err
	}
	if f.size != "" && f.size != sizeFit {
		if _, _, err := parseSize(f.size); err != nil {
			return err
		}
	}
	if f.size != "" && (replay != "" || f.spectate != "") {
		return errors.New("-size sizes the map a run plays; a replay plays its journal's and a spectator watches its session's")
	}
	return nil
}

// spectated is -spectate's session and the slot looked from first, the first when
// it names none.
func (f sessionFlags) spectated() (target string, slot uint8, err error) {
	switch parts := strings.Fields(f.spectate); len(parts) {
	case 0:
	case 1, 2:
		target = parts[0]
		if len(parts) == 2 {
			n, err := mode.ParsePlayerSlot(parts[1])
			if err != nil {
				return "", 0, fmt.Errorf("-spectate: %w", err)
			}
			slot = uint8(n)
		}
	default:
		err = errors.New("-spectate takes a session's address and, after it, one slot")
	}
	return target, slot, err
}

// audience is -spectators as the configuration keeps it: zero for the default when
// the flag is not given, and a negative count for a 0 that takes none.
func (f sessionFlags) audience() (int, error) {
	if f.spectators == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(f.spectators)
	if err != nil || n < 0 || n > parameter.FeedAudienceMax {
		return 0, fmt.Errorf("-spectators %q is not a count from 0 to %d", f.spectators, parameter.FeedAudienceMax)
	}
	return cmp.Or(n, -1), nil
}

// endpoint is a target validateInvocation has already parsed; "" is the zero value.
func endpoint(target string) link.Endpoint {
	e, _ := link.ParseEndpoint(target)
	return e
}

// relayTarget keeps -relay's vif:// form, which names a router to publish the door
// through rather than an address to bind (doc/multi-player.md §5.6).
func relayTarget(target string) string {
	if strings.HasPrefix(target, "vif://") {
		return target
	}
	return endpoint(target).Addr
}

// doorList is a host's doors as validateInvocation read them; "" binds none.
func doorList(target string) string {
	if target == "" {
		return ""
	}
	doors, _ := link.Doors(target)
	return link.DoorList(doors)
}

// doorHosts are the hosts the wss:// doors of -host and -serve name, which a
// certificate made for the run is issued to.
func (f sessionFlags) doorHosts() []string {
	var hosts []string
	for _, target := range []string{f.host, f.serve} {
		doors, _ := link.Doors(target)
		for _, d := range doors {
			if u, err := url.Parse(d.Addr); err == nil && d.Secure() && u.Hostname() != "" {
				hosts = append(hosts, u.Hostname())
			}
		}
	}
	return hosts
}

// validSessionName holds a name to what a URL path, a Kubernetes object name and a
// routing table all accept, which is the same set the fleet's session IDs use.
func validSessionName(name string) error {
	if len(name) > network.MaxSessionName {
		return fmt.Errorf("session name %q is longer than %d characters", name, network.MaxSessionName)
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return fmt.Errorf("session name %q is not lowercase alphanumeric or '-'", name)
		}
	}
	return nil
}

// sizeFit is the -size that takes a run's size from its screen (app.Config.FitSize)
const sizeFit = "fit"

// parseSize reads a WxH geometry for a run that derives none from a terminal.
func parseSize(spec string) (width, height int, err error) {
	w, h, ok := strings.Cut(spec, "x")
	if !ok {
		return 0, 0, fmt.Errorf("-size %q is not WxH, for example 120x40, or %s", spec, sizeFit)
	}
	if width, err = strconv.Atoi(w); err != nil || width <= 0 {
		return 0, 0, fmt.Errorf("-size %q has no usable width", spec)
	}
	if height, err = strconv.Atoi(h); err != nil || height <= 0 {
		return 0, 0, fmt.Errorf("-size %q has no usable height", spec)
	}
	return width, height, nil
}

func validateInvocation(schema, check bool, replay, script string, watch bool, session sessionFlags) error {
	modes := 0
	for _, selected := range []bool{schema, check, replay != "", script != ""} {
		if selected {
			modes++
		}
	}
	if modes > 1 {
		return fmt.Errorf("-schema, -check, -replay and -script are mutually exclusive")
	}
	if err := session.validateInvocation(schema, check, replay); err != nil {
		return err
	}
	if files := session.botFiles(); files != nil && (script != "" || watch) {
		return fmt.Errorf("-bot %s keeps bot files and exits; it takes no run", files[0])
	}
	if watch && script == "" && session.botCount() == 0 {
		return fmt.Errorf("-watch presents a -script or -bot run and has no other subject")
	}
	if watch && session.serve != "" {
		return errors.New("-serve has no terminal; use -host with -watch to watch bots")
	}
	return nil
}

// botCount is how many bots -bot asks for, once it is valid.
func (f sessionFlags) botCount() int {
	bots, _ := app.BotSpecs(f.botSpec())
	return len(bots)
}

// botVerb is -bot's verb and what follows it, as :bot reads them (engine.BotVerb,
// engine.BotWords): no verb where the flag is absent, and a graph given with no
// verb whole, since its path may hold a space.
func (f sessionFlags) botVerb() (string, []string) {
	words := engine.BotWords(f.bot)
	if len(words) == 0 {
		return "", nil
	}
	verb, rest := engine.BotVerb(words)
	if verb == "add" && words[0] != verb && len(rest) > 1 {
		rest = []string{f.bot}
	}
	return verb, rest
}

// botSpec is what -bot seats bots by: nothing unless it adds, and one bot of
// the scenario's own where add names none.
func (f sessionFlags) botSpec() string {
	verb, rest := f.botVerb()
	if verb != "add" {
		return ""
	}
	return cmp.Or(strings.Join(rest, " "), "1")
}

// botFiles is the words of a -bot verb that keeps bot files, nil for any other
func (f sessionFlags) botFiles() []string {
	if verb, rest := f.botVerb(); verb != "" && verb != "add" && verb != "drop" {
		return append([]string{verb}, rest...)
	}
	return nil
}

// Go's flag parser has no optional operand: -bot's words, a graph or a verb with
// all that follows it, and the slot after -spectate's address are folded into
// their flag's value, and the next option kept.
func botArgs(fs *flag.FlagSet, args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || !strings.HasPrefix(arg, "-") {
			return append(out, args[i:]...)
		}
		name := strings.TrimLeft(arg, "-")
		if flag, value, given := strings.Cut(name, "="); flag == "spectate" {
			if !given && i+1 < len(args) {
				i++
				value = args[i]
			}
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				value += " " + args[i]
			}
			out = append(out, "-spectate="+value)
			continue
		}
		if strings.Contains(name, "=") {
			out = append(out, arg)
			continue
		}
		if name == "bot" {
			spec := "1"
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				spec = args[i]
				// A verb's words stay apart, one that holds a space between quotes (engine.BotWords)
				for engine.BotUsage(spec[:strings.IndexAny(spec+" ", " ")]) != "" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
					if word := args[i]; word == "" || strings.ContainsAny(word, " \t") {
						spec += ` "` + word + `"`
					} else {
						spec += " " + word
					}
				}
			}
			out = append(out, "-"+name+"="+spec)
			continue
		}
		out = append(out, arg)
		if f := fs.Lookup(name); f != nil {
			b, ok := f.Value.(interface{ IsBoolFlag() bool })
			if (!ok || !b.IsBoolFlag()) && i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
		}
	}
	return out
}

// --- Flag types ---

type flagParser[T any] func(string, T) (value T, set bool, err error)

// setFlag records whether a parsed value was explicitly supplied.
type setFlag[T any] struct {
	value   T
	set     bool
	boolean bool
	parse   flagParser[T]
}

func newSetFlag[T any](boolean bool, parse flagParser[T]) setFlag[T] {
	return setFlag[T]{boolean: boolean, parse: parse}
}

func (f *setFlag[T]) String() string   { return fmt.Sprint(f.value) }
func (f *setFlag[T]) IsBoolFlag() bool { return f.boolean }

func (f *setFlag[T]) Set(s string) error {
	value, set, err := f.parse(s, f.value)
	if err != nil {
		return err
	}
	f.value, f.set = value, set
	return nil
}

// valueOr returns an explicit value or the supplied default.
func (f *setFlag[T]) valueOr(fallback T) T {
	if f.set {
		return f.value
	}
	return fallback
}

type logFlags struct {
	dir     setFlag[string]
	level   setFlag[string]
	scope   setFlag[string]
	stat    setFlag[int]
	rec     setFlag[int]
	session setFlag[string]
	console bool
}

func newLogFlags() *logFlags {
	return &logFlags{
		dir:     newSetFlag(true, parseOutputDirFlag),
		level:   newSetFlag(false, parseStringFlag),
		scope:   newSetFlag(false, parseScopeFlag),
		stat:    newSetFlag(false, parseTicksFlag),
		rec:     newSetFlag(false, parseTicksFlag),
		session: newSetFlag(false, parseLogSessionIDFlag),
	}
}

// register installs the logging flags and their aliases.
func (f *logFlags) register(fs *flag.FlagSet) {
	for _, alias := range []struct {
		short, long string
		value       flag.Value
	}{
		{"l", "log", &f.dir},
		{"lv", "log-level", &f.level},
		{"ls", "log-scope", &f.scope},
		{"lt", "log-stat", &f.stat},
		{"lr", "log-recorder", &f.rec},
	} {
		fs.Var(alias.value, alias.short, "")
		fs.Var(alias.value, alias.long, "")
	}
	fs.Var(&f.session, "log-session-id", "")
	fs.BoolVar(&f.console, "log-stdout", false, "")
}

// enabled reports whether any logging flag was supplied.
func (f *logFlags) enabled() bool {
	return f.dir.set || f.level.set || f.scope.set || f.stat.set || f.rec.set ||
		f.session.set || f.console
}

// parseOutputDirFlag keeps -l, -j and -mw boolean while accepting =DIR.
func parseOutputDirFlag(s, current string) (string, bool, error) {
	switch s {
	case "false":
		return "", false, nil
	case "", "true":
		return current, true, nil
	default:
		return s, true, nil
	}
}

func parseStringFlag(s, _ string) (string, bool, error) {
	return s, true, nil
}

func parseLogSessionIDFlag(s, _ string) (string, bool, error) {
	if s == "" {
		return "", false, fmt.Errorf("log session ID cannot be empty")
	}
	if err := validSessionName(s); err != nil {
		return "", false, fmt.Errorf("log session ID: %w", err)
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return "", false, fmt.Errorf("log session ID must start and end with a letter or digit")
	}
	return s, true, nil
}

func parseScopeFlag(s, _ string) (string, bool, error) {
	if _, err := vlog.ParseScopes(s, vlog.ScopeAll); err != nil {
		return "", false, err
	}
	return s, true, nil
}

// parseTicksFlag maps an explicit zero to the runtime disable sentinel.
func parseTicksFlag(s string, _ int) (int, bool, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, false, fmt.Errorf("must be a non-negative tick count")
	}
	if n == 0 {
		n = -1
	}
	return n, true, nil
}

func parseBoolFlag(s string, _ bool) (bool, bool, error) {
	b, err := strconv.ParseBool(s)
	return b, err == nil, err
}
