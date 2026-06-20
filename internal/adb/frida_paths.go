package adb

import (
	"os"
	"path/filepath"
)

// Host-side Frida Manager storage layout.
//
// We deliberately split large/disposable artifacts from user-authored data,
// mirroring the rest of the codebase (frida-server binaries already live under
// the OS cache dir via fridaCacheDir; profiles/hosts live under ~/.adbq):
//
//   <UserCacheDir>/adbq/frida/venvs/<ver>/   managed Python venvs (one per frida version)
//   <UserCacheDir>/adbq/frida/wheels/        verified frida wheels downloaded from PyPI
//   ~/.adbq/frida/                           runtime.json, scripts.json, app-scripts.json
//   ~/.adbq/frida/scripts/<id>.js            script library bodies
//
// A torn write to a cache file just triggers a re-download; a torn write to the
// config bucket would lose the user's work, so those use atomic writes.

// fridaVenvsDir holds the managed virtualenvs. Disposable → cache dir.
func fridaVenvsDir() (string, error) { return fridaCacheSub("venvs") }

// fridaWheelsDir caches verified frida wheels from PyPI. Disposable → cache dir.
func fridaWheelsDir() (string, error) { return fridaCacheSub("wheels") }

func fridaCacheSub(name string) (string, error) {
	base, err := fridaCacheDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// fridaDataDir holds Frida config/index JSON (runtime.json, and later
// scripts.json / app-scripts.json). User-authored → ~/.adbq, atomic writes.
func fridaDataDir() (string, error) {
	base, err := configDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "frida")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}
