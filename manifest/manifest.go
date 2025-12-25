package manifest

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/registry"
	"github.com/lixenwraith/vi-fighter/render"
	"github.com/lixenwraith/vi-fighter/render/renderers"
	"github.com/lixenwraith/vi-fighter/system"
)

// RegisterComponents registers all component types with the World
// Must be called before systems are created
func RegisterComponents(w *engine.World) {
	engine.RegisterComponent[component.CharacterComponent](w)
	engine.RegisterComponent[component.FlashComponent](w)
	engine.RegisterComponent[component.NuggetComponent](w)
	engine.RegisterComponent[component.DrainComponent](w)
	engine.RegisterComponent[component.CursorComponent](w)
	engine.RegisterComponent[component.ProtectionComponent](w)
	engine.RegisterComponent[component.PingComponent](w)
	engine.RegisterComponent[component.ShieldComponent](w)
	engine.RegisterComponent[component.BoostComponent](w)
	engine.RegisterComponent[component.SplashComponent](w)
	engine.RegisterComponent[component.DeathComponent](w)
	engine.RegisterComponent[component.TimerComponent](w)
	engine.RegisterComponent[component.EnergyComponent](w)
	engine.RegisterComponent[component.HeatComponent](w)
	engine.RegisterComponent[component.DecayComponent](w)
	engine.RegisterComponent[component.CleanerComponent](w)
	engine.RegisterComponent[component.MaterializeComponent](w)
	engine.RegisterComponent[component.TypeableComponent](w)
	engine.RegisterComponent[component.CompositeHeaderComponent](w)
	engine.RegisterComponent[component.MemberComponent](w)
	engine.RegisterComponent[component.BlossomComponent](w)
}

// RegisterSystems registers all system factories with the registry
func RegisterSystems() {
	registry.RegisterSystem("ping", func(w any) any {
		return system.NewPingSystem(w.(*engine.World))
	})
	registry.RegisterSystem("energy", func(w any) any {
		return system.NewEnergySystem(w.(*engine.World))
	})
	registry.RegisterSystem("shield", func(w any) any {
		return system.NewShieldSystem(w.(*engine.World))
	})
	registry.RegisterSystem("heat", func(w any) any {
		return system.NewHeatSystem(w.(*engine.World))
	})
	registry.RegisterSystem("boost", func(w any) any {
		return system.NewBoostSystem(w.(*engine.World))
	})
	registry.RegisterSystem("typing", func(w any) any {
		return system.NewTypingSystem(w.(*engine.World))
	})
	registry.RegisterSystem("composite", func(w any) any {
		return system.NewCompositeSystem(w.(*engine.World))
	})
	registry.RegisterSystem("spawn", func(w any) any {
		return system.NewSpawnSystem(w.(*engine.World))
	})
	registry.RegisterSystem("nugget", func(w any) any {
		return system.NewNuggetSystem(w.(*engine.World))
	})
	registry.RegisterSystem("decay", func(w any) any {
		return system.NewDecaySystem(w.(*engine.World))
	})
	registry.RegisterSystem("blossom", func(w any) any {
		return system.NewBlossomSystem(w.(*engine.World))
	})
	registry.RegisterSystem("gold", func(w any) any {
		return system.NewGoldSystem(w.(*engine.World))
	})
	registry.RegisterSystem("materialize", func(w any) any {
		return system.NewMaterializeSystem(w.(*engine.World))
	})
	registry.RegisterSystem("drain", func(w any) any {
		return system.NewDrainSystem(w.(*engine.World))
	})
	registry.RegisterSystem("cleaner", func(w any) any {
		return system.NewCleanerSystem(w.(*engine.World))
	})
	registry.RegisterSystem("flash", func(w any) any {
		return system.NewFlashSystem(w.(*engine.World))
	})
	registry.RegisterSystem("splash", func(w any) any {
		return system.NewSplashSystem(w.(*engine.World))
	})
	registry.RegisterSystem("death", func(w any) any {
		return system.NewDeathSystem(w.(*engine.World))
	})
	registry.RegisterSystem("timekeeper", func(w any) any {
		return system.NewTimeKeeperSystem(w.(*engine.World))
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
		"typing",
		"composite",
		"spawn",
		"nugget",
		"decay",
		"blossom",
		"gold",
		"materialize",
		"drain",
		"cleaner",
		"flash",
		"splash",
		"death",
		"timekeeper",
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