package manifest

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/registry"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/render/renderers"
	"github.com/lixenwraith/vi-fighter/systems"
)

// RegisterComponents registers all component types with the World
// Must be called before systems are created
func RegisterComponents(w *engine.World) {
	engine.RegisterComponent[components.CharacterComponent](w)
	engine.RegisterComponent[components.SequenceComponent](w)
	engine.RegisterComponent[components.GoldSequenceComponent](w)
	engine.RegisterComponent[components.FlashComponent](w)
	engine.RegisterComponent[components.NuggetComponent](w)
	engine.RegisterComponent[components.DrainComponent](w)
	engine.RegisterComponent[components.CursorComponent](w)
	engine.RegisterComponent[components.ProtectionComponent](w)
	engine.RegisterComponent[components.PingComponent](w)
	engine.RegisterComponent[components.ShieldComponent](w)
	engine.RegisterComponent[components.BoostComponent](w)
	engine.RegisterComponent[components.SplashComponent](w)
	engine.RegisterComponent[components.DeathComponent](w)
	engine.RegisterComponent[components.TimerComponent](w)
	engine.RegisterComponent[components.EnergyComponent](w)
	engine.RegisterComponent[components.HeatComponent](w)
	engine.RegisterComponent[components.DecayComponent](w)
	engine.RegisterComponent[components.CleanerComponent](w)
	engine.RegisterComponent[components.MaterializeComponent](w)
}

// RegisterSystems registers all system factories with the registry
func RegisterSystems() {
	registry.RegisterSystem("ping", func(w any) any {
		return systems.NewPingSystem(w.(*engine.World))
	})
	registry.RegisterSystem("energy", func(w any) any {
		return systems.NewEnergySystem(w.(*engine.World))
	})
	registry.RegisterSystem("shield", func(w any) any {
		return systems.NewShieldSystem(w.(*engine.World))
	})
	registry.RegisterSystem("heat", func(w any) any {
		return systems.NewHeatSystem(w.(*engine.World))
	})
	registry.RegisterSystem("boost", func(w any) any {
		return systems.NewBoostSystem(w.(*engine.World))
	})
	registry.RegisterSystem("spawn", func(w any) any {
		return systems.NewSpawnSystem(w.(*engine.World))
	})
	registry.RegisterSystem("nugget", func(w any) any {
		return systems.NewNuggetSystem(w.(*engine.World))
	})
	registry.RegisterSystem("decay", func(w any) any {
		return systems.NewDecaySystem(w.(*engine.World))
	})
	registry.RegisterSystem("gold", func(w any) any {
		return systems.NewGoldSystem(w.(*engine.World))
	})
	registry.RegisterSystem("materialize", func(w any) any {
		return systems.NewMaterializeSystem(w.(*engine.World))
	})
	registry.RegisterSystem("drain", func(w any) any {
		return systems.NewDrainSystem(w.(*engine.World))
	})
	registry.RegisterSystem("cleaner", func(w any) any {
		return systems.NewCleanerSystem(w.(*engine.World))
	})
	registry.RegisterSystem("flash", func(w any) any {
		return systems.NewFlashSystem(w.(*engine.World))
	})
	registry.RegisterSystem("splash", func(w any) any {
		return systems.NewSplashSystem(w.(*engine.World))
	})
	registry.RegisterSystem("timekeeper", func(w any) any {
		return systems.NewTimeKeeperSystem(w.(*engine.World))
	})
	registry.RegisterSystem("cull", func(w any) any {
		return systems.NewCullSystem(w.(*engine.World))
	})
}

// RegisterRenderers registers all renderer factories with priorities
func RegisterRenderers() {
	registry.RegisterRenderer("ping", func(ctx any) any {
		return renderers.NewPingRenderer(ctx.(*engine.GameContext))
	}, render.PriorityGrid)

	registry.RegisterRenderer("splash", func(ctx any) any {
		return renderers.NewSplashRenderer(ctx.(*engine.GameContext))
	}, render.PrioritySplash)

	registry.RegisterRenderer("characters", func(ctx any) any {
		return renderers.NewCharactersRenderer(ctx.(*engine.GameContext))
	}, render.PriorityEntities)

	registry.RegisterRenderer("shield", func(ctx any) any {
		return renderers.NewShieldRenderer(ctx.(*engine.GameContext))
	}, render.PriorityEffects)

	registry.RegisterRenderer("effects", func(ctx any) any {
		return renderers.NewEffectsRenderer(ctx.(*engine.GameContext))
	}, render.PriorityEffects)

	registry.RegisterRenderer("drain", func(ctx any) any {
		return renderers.NewDrainRenderer(ctx.(*engine.GameContext))
	}, render.PriorityDrain)

	registry.RegisterRenderer("grayout", func(ctx any) any {
		return renderers.NewGrayoutRenderer(ctx.(*engine.GameContext))
	}, render.PriorityUI-10)

	registry.RegisterRenderer("dim", func(ctx any) any {
		return renderers.NewDimRenderer(ctx.(*engine.GameContext))
	}, render.PriorityUI-5)

	registry.RegisterRenderer("heatmeter", func(ctx any) any {
		return renderers.NewHeatMeterRenderer(ctx.(*engine.GameContext))
	}, render.PriorityUI)

	registry.RegisterRenderer("linenumbers", func(ctx any) any {
		return renderers.NewLineNumbersRenderer(ctx.(*engine.GameContext))
	}, render.PriorityUI)

	registry.RegisterRenderer("columnindicators", func(ctx any) any {
		return renderers.NewColumnIndicatorsRenderer(ctx.(*engine.GameContext))
	}, render.PriorityUI)

	registry.RegisterRenderer("statusbar", func(ctx any) any {
		return renderers.NewStatusBarRenderer(ctx.(*engine.GameContext))
	}, render.PriorityUI)

	registry.RegisterRenderer("cursor", func(ctx any) any {
		return renderers.NewCursorRenderer(ctx.(*engine.GameContext))
	}, render.PriorityUI)

	registry.RegisterRenderer("overlay", func(ctx any) any {
		return renderers.NewOverlayRenderer(ctx.(*engine.GameContext))
	}, render.PriorityOverlay)
}

// ActiveSystems returns the ordered list of systems to instantiate
// Order matters for event handler registration priority
func ActiveSystems() []string {
	return []string{
		"ping",
		"energy",
		"shield",
		"heat",
		"boost",
		"spawn",
		"nugget",
		"decay",
		"gold",
		"materialize",
		"drain",
		"cleaner",
		"flash",
		"splash",
		"timekeeper",
		"cull",
	}
}

// ActiveRenderers returns the ordered list of renderers to instantiate
func ActiveRenderers() []string {
	return []string{
		"ping",
		"splash",
		"characters",
		"shield",
		"effects",
		"drain",
		"grayout",
		"dim",
		"heatmeter",
		"linenumbers",
		"columnindicators",
		"statusbar",
		"cursor",
		"overlay",
	}
}