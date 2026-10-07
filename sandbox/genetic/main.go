package main

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lixenwraith/vif/pkg/genetic"
	"github.com/lixenwraith/vif/pkg/genetic/fitness"
	"github.com/lixenwraith/vif/pkg/genetic/registry"
	"github.com/lixenwraith/vif/pkg/genetic/tracking"
)

const (
	screenWidth = 80
	simHeight   = 18
	groundY     = 0.0

	maxTicks  = 300
	dt        = 16 * time.Millisecond
	gravity   = 0.04
	maxThrust = 0.08

	// One fixed initial condition. The controller no longer has to solve
	// two mirrored tasks simultaneously.
	startX  = 10.0
	startY  = simHeight - 2.0
	startVx = 0.0

	simCount   = 32
	speciesLID = 1
)

// Genome:
//
//	[0] horizontal Kp
//	[1] horizontal Kd
//	[2] vertical Kp
//	[3] vertical Kd
//
// Controller signs are fixed by the equations below, so the GA searches only
// physically meaningful positive gains.
const (
	geneKpX = iota
	geneKdX
	geneKpY
	geneKdY
	geneCount
)

type Lander struct {
	id    uint64
	genes []float64

	x, y   float64
	vx, vy float64
	fuel   float64
	ticks  int

	active bool
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	rawMode := exec.Command("stty", "-icanon", "-echo")
	rawMode.Stdin = os.Stdin
	_ = rawMode.Run()

	fmt.Print("\033[?25l\033[2J")
	defer func() {
		reset := exec.Command("stty", "icanon", "echo")
		reset.Stdin = os.Stdin
		_ = reset.Run()
		fmt.Print("\033[?25h\033[2J\033[H")
	}()

	var targetPos atomic.Int32
	targetPos.Store(40)

	resetEnv := make(chan struct{}, 1)

	go func() {
		b := make([]byte, 3)
		for {
			n, err := os.Stdin.Read(b)
			if err != nil {
				return
			}
			if n == 0 {
				continue
			}

			if b[0] == 'q' || b[0] == 'Q' {
				cancel()
				return
			}

			move := 0
			if b[0] == 'h' || b[0] == 'a' ||
				(n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'D') {
				move = -3
			}
			if b[0] == 'l' || b[0] == 'd' ||
				(n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'C') {
				move = 3
			}
			if move == 0 {
				continue
			}

			old := targetPos.Load()
			next := old + int32(move)
			if next < 5 {
				next = 5
			}
			if next > screenWidth-6 {
				next = screenWidth - 6
			}
			if next == old {
				continue
			}

			targetPos.Store(next)

			// A target change is a new optimization problem. Discard both
			// queued proposals and scored candidates from the old objective.
			select {
			case resetEnv <- struct{}{}:
			default:
			}
		}
	}()

	cfg := genetic.DefaultStreamingConfig()
	cfg.PoolSize = 48
	cfg.ProposalCapacity = simCount
	cfg.PendingCapacity = simCount * 2
	cfg.MinOutcomesPerGen = simCount

	// Conservative mutation lets good controllers be refined instead of
	// constantly being thrown far away from the current basin.
	cfg.PerturbationRate = 0.20
	cfg.PerturbationStrength = 0.06
	cfg.Seed = uint64(time.Now().UnixNano())

	reg := registry.NewRegistry(nil)

	sCfg := registry.SpeciesConfig{
		ID:        speciesLID,
		Name:      "lander",
		GeneCount: geneCount,
		Bounds: []genetic.ParameterBounds{
			{Min: 0.0, Max: 4.0}, // horizontal Kp
			{Min: 0.0, Max: 8.0}, // horizontal Kd
			{Min: 0.0, Max: 4.0}, // vertical Kp
			{Min: 0.0, Max: 8.0}, // vertical Kd
		},
		TournamentSize: 3,
		MixProbability: 0.5,
		EngineConfig:   &cfg,
	}

	// Landing position and touchdown speed dominate. Fuel is only a
	// secondary tie-breaker, so "do nothing" is not an attractive solution.
	agg := &fitness.WeightedAggregator{
		Weights: map[string]float64{
			"landing_distance": 0.65,
			"landing_speed":    0.25,
			"fuel_used":        0.10,
		},
		Normalizers: map[string]fitness.NormalizeFunc{
			"landing_distance": fitness.NormalizeInverse(4.0),
			"landing_speed":    fitness.NormalizeInverse(0.25),
			"fuel_used":        fitness.NormalizeInverse(10.0),
		},
	}

	if err := reg.Register(sCfg, agg); err != nil {
		panic(err)
	}
	reg.Start()
	defer reg.Stop()

	var landers [simCount]Lander
	buf := bytes.NewBuffer(make([]byte, 0, screenWidth*simHeight+2048))
	ticker := time.NewTicker(dt)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-resetEnv:
			ts := reg.GetTracker(speciesLID)
			ts.Engine.Inject(nil, 0)

			for i := range landers {
				if landers[i].active {
					reg.AbandonFitness(speciesLID, landers[i].id)
					landers[i].active = false
				}
			}
		}

		targetX := float64(targetPos.Load())

		for i := range landers {
			if landers[i].active {
				continue
			}

			genes, evalID := reg.Sample(speciesLID)
			if evalID == 0 {
				continue
			}
			reg.BeginTracking(speciesLID, evalID)

			landers[i] = Lander{
				id:     evalID,
				genes:  genes,
				x:      startX,
				y:      startY,
				vx:     startVx,
				vy:     0,
				active: true,
			}
		}

		for i := range landers {
			l := &landers[i]
			if !l.active {
				continue
			}

			// Horizontal PD:
			// Kp accelerates toward the target; Kd damps vx.
			errX := (targetX - l.x) / screenWidth
			normVx := l.vx / 2.0
			reqX := l.genes[geneKpX]*errX - l.genes[geneKdX]*normVx

			// Vertical PD toward the ground.  The previous version used
			// positive altitude as the position error, which made positive KpY
			// accelerate upward.  The GA consequently discovered that falling
			// straight down was a perfectly stable local basin.
			normY := (groundY - l.y) / simHeight
			normVy := l.vy / 2.0
			reqY := l.genes[geneKpY]*normY - l.genes[geneKdY]*normVy

			thrustX := clamp(reqX*maxThrust, -maxThrust, maxThrust)
			thrustY := clamp(reqY*maxThrust, -maxThrust, maxThrust)

			l.fuel += math.Abs(thrustX) + math.Abs(thrustY)
			l.vx += thrustX
			l.vy += thrustY - gravity
			l.x += l.vx
			l.y += l.vy
			l.ticks++

			outOfBounds := l.x < 0 || l.x >= screenWidth || l.y > simHeight
			landed := l.y <= groundY
			timeout := l.ticks >= maxTicks

			if !landed && !outOfBounds && !timeout {
				continue
			}

			// At terminal evaluation y is already at/below the ground, so
			// horizontal miss distance is the cleanest position objective.
			landingDistance := math.Abs(targetX - l.x)
			landingSpeed := math.Hypot(l.vx, l.vy)

			// Leaving the sandbox is much worse than an ordinary miss.
			if outOfBounds {
				landingDistance += float64(screenWidth)
			}

			deathMetrics := tracking.MetricBundle{
				"landing_distance": landingDistance,
				"landing_speed":    landingSpeed,
				"fuel_used":        l.fuel,
			}
			reg.CompleteTracking(speciesLID, l.id, deathMetrics, fitness.MapContext{})
			l.active = false
		}

		activeCount := 0
		for i := range landers {
			if landers[i].active {
				activeCount++
			}
		}

		render(buf, landers[:], reg, int(targetX), activeCount)
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func render(buf *bytes.Buffer, landers []Lander, reg *registry.Registry, targetX int, activeCount int) {
	buf.Reset()
	buf.WriteString("\033[H") // Cursor Home

	stats := reg.Stats(speciesLID)
	ts := reg.GetTracker(speciesLID)

	var bestGenes []float64
	if best, ok := ts.Engine.Best(); ok {
		bestGenes = best.Data
	}

	// \033[K erases from the cursor to the end of the line, fixing trailing text
	buf.WriteString(fmt.Sprintf(
		"\033[1;37m GA Lander Sandbox \033[0m| Gen: \033[36m%d\033[0m | Archive: \033[36m%d\033[0m | Active: \033[36m%d\033[0m\n",
		stats.Generation, stats.PoolSize, activeCount,
	))
	buf.WriteString(fmt.Sprintf(
		" Fit  | Best: \033[32m%.4f\033[0m | Avg: \033[36m%.4f\033[0m | Worst: \033[31m%.4f\033[0m | Target X: \033[32m%d\033[0m\033[K\n",
		stats.BestFitness, stats.AvgFitness, stats.WorstFitness, targetX,
	))
	buf.WriteString(fmt.Sprintf(
		" Stat | Evals: \033[36m%d\033[0m | Evicted: \033[31m%d\033[0m | Diversity: \033[36m%.3f\033[0m\033[K\n",
		stats.TotalEvals, stats.Evicted, stats.Diversity,
	))

	if len(bestGenes) >= geneCount {
		buf.WriteString(fmt.Sprintf(
			" Best | X [Kp:\033[36m%+5.2f\033[0m Kd:\033[36m%+5.2f\033[0m] Y [Kp:\033[36m%+5.2f\033[0m Kd:\033[36m%+5.2f\033[0m]\033[K\n",
			bestGenes[geneKpX], bestGenes[geneKdX],
			bestGenes[geneKpY], bestGenes[geneKdY],
		))
	} else {
		buf.WriteString(" Best | Waiting for evaluations...\033[K\n")
	}

	buf.WriteString("\033[K\n") // Spacer line

	buf.WriteString(fmt.Sprintf(
		" Press '\033[33mq\033[0m' to quit, '\033[33m< >\033[0m' to move\033[K\n",
	))

	buf.WriteString("\033[K\n") // Spacer line

	var screen [simHeight][screenWidth]byte
	for y := 0; y < simHeight; y++ {
		for x := 0; x < screenWidth; x++ {
			screen[y][x] = ' '
		}
	}

	// Fancier landing pad: [===o===]
	for i := -4; i <= 4; i++ {
		tx := targetX + i
		if tx >= 0 && tx < screenWidth {
			switch i {
			case -4:
				screen[simHeight-1][tx] = '['
			case 4:
				screen[simHeight-1][tx] = ']'
			case 0:
				screen[simHeight-1][tx] = 'o'
			default:
				screen[simHeight-1][tx] = '='
			}
		}
	}

	for _, l := range landers {
		if !l.active {
			continue
		}
		x := int(l.x)
		y := simHeight - 1 - int(l.y)
		if x >= 0 && x < screenWidth && y >= 0 && y < simHeight {
			// If crashed on the ground outside the pad, show an 'X'
			if y == simHeight-1 && screen[y][x] == ' ' {
				screen[y][x] = 'X'
			} else if screen[y][x] == ' ' {
				screen[y][x] = 'A'
			}
		}
	}

	for y := 0; y < simHeight; y++ {
		for x := 0; x < screenWidth; x++ {
			switch screen[y][x] {
			case 'A': // Flying lander
				buf.WriteString("\033[1;33mA\033[0m")
			case 'X': // Crashed lander
				buf.WriteString("\033[1;31mX\033[0m")
			case '=': // Pad surface
				buf.WriteString("\033[1;32m=\033[0m")
			case 'o': // Pad center
				buf.WriteString("\033[1;31mo\033[0m")
			case '[', ']': // Pad edges
				buf.WriteString("\033[1;37m")
				buf.WriteByte(screen[y][x])
				buf.WriteString("\033[0m")
			default:
				buf.WriteByte(screen[y][x])
			}
		}
		buf.WriteString("\033[K\n")
	}

	_, _ = os.Stdout.Write(buf.Bytes())
}
