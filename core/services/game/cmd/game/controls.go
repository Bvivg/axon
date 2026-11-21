package main

import (
	"errors"

	"github.com/bvivg/axon/core/shared/pkg/config"
)

var (
	errHooksOutsideTest = errors.New("test hooks are allowed only with APP_ENV=test")
	errShortInternalKey = errors.New("must be at least 32 characters")
)

func loadControls(loader *config.Loader, env config.Environment) (hooks bool, ownerKey string) {
	hooks = loader.Bool("GAME_TEST_HOOKS", false)
	if hooks && env != config.EnvTest {
		loader.Fail("GAME_TEST_HOOKS", errHooksOutsideTest)
		hooks = false
	}
	ownerKey = loader.SecretDefault("GAME_INTERNAL_KEY", "").Reveal()
	if ownerKey != "" && len(ownerKey) < 32 {
		loader.Fail("GAME_INTERNAL_KEY", errShortInternalKey)
	}
	return hooks, ownerKey
}
