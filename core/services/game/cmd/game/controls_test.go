package main

import (
	"errors"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

func TestTestHooksNeedTheTestEnvironment(t *testing.T) {
	for env, allowed := range map[config.Environment]bool{config.EnvTest: true, config.EnvDevelopment: false, config.EnvProduction: false} {
		loader := config.NewLoaderFromMap(map[string]string{"GAME_TEST_HOOKS": "true"})

		hooks, _ := loadControls(loader, env)

		err := loader.Err()
		if allowed && (err != nil || !hooks) {
			t.Fatalf("APP_ENV=%s: hooks=%v err=%v, want hooks on", env, hooks, err)
		}
		if !allowed && !errors.Is(err, errHooksOutsideTest) {
			t.Fatalf("APP_ENV=%s: err=%v, want errHooksOutsideTest", env, err)
		}
	}
}

func TestHooksOffNeedNothing(t *testing.T) {
	loader := config.NewLoaderFromMap(nil)

	hooks, key := loadControls(loader, config.EnvDevelopment)

	if err := loader.Err(); err != nil || hooks || key != "" {
		t.Fatalf("hooks=%v key=%q err=%v", hooks, key, err)
	}
}

func TestInternalKeyMustBeLongEnough(t *testing.T) {
	loader := config.NewLoaderFromMap(map[string]string{"GAME_INTERNAL_KEY": "short"})

	loadControls(loader, config.EnvDevelopment)

	if loader.Err() == nil {
		t.Fatal("a short GAME_INTERNAL_KEY was accepted")
	}

	loader = config.NewLoaderFromMap(map[string]string{"GAME_INTERNAL_KEY": "0123456789abcdef0123456789abcdef"})
	if _, key := loadControls(loader, config.EnvDevelopment); loader.Err() != nil || key != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("key=%q err=%v", key, loader.Err())
	}
}
