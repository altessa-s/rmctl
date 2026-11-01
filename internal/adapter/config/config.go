// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/altessa-s/rmcloud-go"
)

// The environment variables rmctl reads. The rmcloud-go host variables
// (RMCLOUD_HOST, RMCLOUD_AUTH_HOST, RMCLOUD_SYNC_HOST) are read as well.
const (
	EnvProfile   = "RMCTL_PROFILE"
	EnvConfigDir = "RMCTL_CONFIG_DIR"
	EnvCacheDir  = "RMCTL_CACHE_DIR"
)

// DefaultProfile is the profile used when none is named.
const DefaultProfile = "default"

// appDir is the directory rmctl keeps under the system's base directories.
const appDir = "rmctl"

// tokenFile is the name of the token file inside a profile's directory.
const tokenFile = "token.json"

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ErrProfile means a profile name is not a safe directory name.
var ErrProfile = errors.New("a profile name is 1-64 characters of a-z, 0-9, _ and -, starting with a letter or digit")

// Config is where a profile keeps its token and the cache, and which hosts
// it talks to.
type Config struct {
	Profile string
	// ConfigDir holds the profile's token file.
	ConfigDir string
	// CacheDir is the cache directory of every profile; each profile's
	// cache is the subdirectory named after it, and shares nothing.
	CacheDir string
	// CacheTTL is how long a confirmed root serves reads without asking the
	// cloud, and Offline whether --offline is the profile's default.
	CacheTTL  time.Duration
	Offline   bool
	TokenPath string
	Endpoints rmcloud.Endpoints
	// ConfigFile is the configuration file read, present or not.
	ConfigFile string
	// Secrets is the token backend, BackendFile or BackendKeychain, and
	// SecretsSource what chose it.
	Secrets       string
	SecretsSource string
	// KeychainService is RMCTL_KEYCHAIN_SERVICE, or empty for the default.
	KeychainService string
}

// BaseDirs are the system's per-user configuration and cache directories.
type BaseDirs struct {
	Config string
	Cache  string
}

// SystemDirs are the base directories of this machine and user.
func SystemDirs() (BaseDirs, error) {
	cfg, err := os.UserConfigDir()
	if err != nil {
		return BaseDirs{}, fmt.Errorf("find the configuration directory: %w", err)
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return BaseDirs{}, fmt.Errorf("find the cache directory: %w", err)
	}
	return BaseDirs{Config: cfg, Cache: cache}, nil
}

// Load resolves the configuration from env (not the process environment),
// the --profile flag and the base directories, without a configuration
// file. The flag wins over RMCTL_PROFILE; RMCTL_CONFIG_DIR and
// RMCTL_CACHE_DIR replace the rmctl directories under dirs.
func Load(env map[string]string, profileFlag string, dirs BaseDirs) (Config, error) {
	return LoadFrom(env, profileFlag, dirs, nil)
}

// LoadFrom is Load with the configuration file read through read: the file
// is RMCTL_CONFIG_FILE, or config.toml in the rmctl configuration directory.
// A nil read reads no file. The token backend is RMCTL_SECRETS, then the
// profile's table, then the file's [secrets], then the file backend.
func LoadFrom(env map[string]string, profileFlag string, dirs BaseDirs, read ReadFunc) (Config, error) {
	profile := profileFlag
	if profile == "" {
		profile = env[EnvProfile]
	}
	if profile == "" {
		profile = DefaultProfile
	}
	if !profileName.MatchString(profile) {
		return Config{}, fmt.Errorf("profile %q: %w", profile, ErrProfile)
	}
	configRoot := env[EnvConfigDir]
	if configRoot == "" {
		if dirs.Config == "" {
			return Config{}, errors.New("no configuration directory: set " + EnvConfigDir)
		}
		configRoot = filepath.Join(dirs.Config, appDir)
	}
	cacheDir := env[EnvCacheDir]
	if cacheDir == "" {
		if dirs.Cache == "" {
			return Config{}, errors.New("no cache directory: set " + EnvCacheDir)
		}
		cacheDir = filepath.Join(dirs.Cache, appDir)
	}
	endpoints, err := rmcloud.FromEnvironment(env)
	if err != nil {
		return Config{}, fmt.Errorf("hosts: %w", err)
	}
	configPath := env[EnvConfigFile]
	if configPath == "" {
		configPath = filepath.Join(configRoot, configFile)
	}
	var file File
	if read != nil {
		data, exists, rerr := read(configPath)
		if rerr != nil {
			return Config{}, rerr
		}
		if exists {
			if file, err = ParseFile(data); err != nil {
				return Config{}, fmt.Errorf("%s: %w", configPath, err)
			}
		}
	}
	backend, source, err := backendOf(env, file, profile)
	if err != nil {
		return Config{}, err
	}
	ttl, offline, err := cacheOf(env, file, profile)
	if err != nil {
		return Config{}, err
	}
	configDir := filepath.Join(configRoot, profile)
	return Config{
		CacheTTL:        ttl,
		Offline:         offline,
		Profile:         profile,
		ConfigDir:       configDir,
		CacheDir:        cacheDir,
		TokenPath:       filepath.Join(configDir, tokenFile),
		Endpoints:       endpoints,
		ConfigFile:      configPath,
		Secrets:         backend,
		SecretsSource:   source,
		KeychainService: env[EnvKeychainService],
	}, nil
}

// Environ is the process environment as a map, for Load.
func Environ() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if key, value, ok := strings.Cut(kv, "="); ok {
			env[key] = value
		}
	}
	return env
}
