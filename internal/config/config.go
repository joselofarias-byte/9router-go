package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/viper"

	"9router/proxy/internal/constants"
	"9router/proxy/internal/log"
)

// Config holds the proxy gateway configuration.
type Config struct {
	Host            string
	Port            int
	DatabasePath    string
	JWTSecret       string
	InitialPassword               string
	APIKeySecret                  string
	MachineIDSalt                  string
	GoogleDriveBackupClientID     string
	GoogleDriveBackupClientSecret string
	RTKEnabled                    bool
	CavemanEnabled  bool
	PonytailEnabled bool
}

// newBaseViper configures environment/default precedence without loading a file.
func newBaseViper() *viper.Viper {
	v := viper.New()
	v.AutomaticEnv()

	v.SetDefault("PORT", 20130)
	v.SetDefault("API_KEY_SECRET", "endpoint-proxy-api-key-secret")
	v.SetDefault("MACHINE_ID_SALT", "endpoint-proxy-salt")
	v.SetDefault("RTK_ENABLED", true)
	v.SetDefault("CAVEMAN_ENABLED", false)
	v.SetDefault("PONYTAIL_ENABLED", false)
	return v
}

// mergeEnvFiles loads lower-priority files first and higher-priority files
// afterwards. OS environment variables still win because AutomaticEnv has
// higher precedence than config files.
func mergeEnvFiles(v *viper.Viper, candidates []string) {
	if v == nil {
		return
	}
	seen := map[string]struct{}{}
	loaded := false

	for i := len(candidates) - 1; i >= 0; i-- {
		path := strings.TrimSpace(candidates[i])
		if path == "" {
			continue
		}
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		if _, err := os.Stat(path); err != nil {
			continue
		}

		v.SetConfigFile(path)
		v.SetConfigType("env")
		var err error
		if loaded {
			err = v.MergeInConfig()
		} else {
			err = v.ReadInConfig()
		}
		if err != nil {
			log.Warn("config", "read config file failed", "file", path, "error", err)
			continue
		}
		loaded = true
	}
}

// NewViper creates a configured Viper instance. A .env in the working
// directory has highest file precedence for development/compose setups; the
// .env next to the executable is the stable fallback for installed daemons.
func NewViper() *viper.Viper {
	v := newBaseViper()
	mergeEnvFiles(v, envFileCandidates())
	return v
}

// NewViperWithFile creates and configures a new Viper instance with one
// explicit env file path. Tests and callers that need a fixed source use this.
func NewViperWithFile(configFile string) *viper.Viper {
	v := newBaseViper()
	if configFile == "" {
		return v
	}
	v.SetConfigFile(configFile)
	v.SetConfigType("env")
	if err := v.ReadInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.Is(err, os.ErrNotExist) && !os.IsNotExist(err) && !errors.As(err, &configFileNotFoundError) {
			log.Warn("config", "read config file failed", "file", configFile, "error", err)
		}
	}
	return v
}

// ProvideViper returns a configured Viper instance for dependency injection.
func ProvideViper() *viper.Viper {
	return NewViper()
}

// ProvideConfig provides *Config for dependency injection using the provided Viper instance.
func ProvideConfig(v *viper.Viper) *Config {
	return LoadConfigFromViper(v)
}

// ResolveDataDir returns the base data directory: DATA_DIR env, else the
// value from .env, else the platform default (~/.9router, or
// %APPDATA%/9router on Windows).
//
// The .env read is load-bearing, not a convenience. The server resolves its
// data dir through viper (LoadConfigFromViper), which does read .env, while
// `9router-go status|stop|logs` runs in a separate short-lived process that
// never boots the server. Reading only os.Getenv here meant a deployment
// configured purely through .env — every Docker/compose setup — had the CLI
// look at a different directory than the running daemon: status reported "not
// running" against a live listener, stop refused to kill it, and logs claimed
// no log existed. Both sides now answer the same question the same way.
func ResolveDataDir() string {
	if dataDir := strings.TrimSpace(os.Getenv("DATA_DIR")); dataDir != "" {
		return dataDir
	}
	if dataDir := dataDirFromEnvFile(); dataDir != "" {
		return dataDir
	}
	if homeDir, err := os.UserHomeDir(); err == nil {
		if runtime.GOOS == "windows" {
			appData := os.Getenv("APPDATA")
			if appData == "" {
				appData = filepath.Join(homeDir, "AppData", "Roaming")
			}
			return filepath.Join(appData, "9router")
		}
		return filepath.Join(homeDir, ".9router")
	}
	return ".9router"
}

// dataDirFromEnvFile reads DATA_DIR out of the .env next to the binary or in
// the working directory. Viper owns the file format, so this reuses it rather
// than hand-parsing KEY=VALUE and drifting from what the server accepted.
func dataDirFromEnvFile() string {
	for _, path := range envFileCandidates() {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		v := viper.New()
		v.SetConfigFile(path)
		v.SetConfigType("env")
		if err := v.ReadInConfig(); err != nil {
			continue
		}
		if dir := strings.TrimSpace(v.GetString("DATA_DIR")); dir != "" {
			return dir
		}
	}
	return ""
}

// envFileCandidates lists the .env locations the server itself reads: the one
// in the working directory, then the one beside the executable so a daemon
// started from another directory still finds the same config its CLI reports on.
func envFileCandidates() []string {
	candidates := []string{".env"}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	return candidates
}

// LoadConfig loads the configuration from environment variables, .env file, and platform defaults using Viper.
func LoadConfig() *Config {
	return LoadConfigFromViper(NewViper())
}

// LoadConfigFromViper builds *Config using the given Viper instance.
func LoadConfigFromViper(v *viper.Viper) *Config {
	if v == nil {
		v = NewViper()
	}
	host := strings.TrimSpace(v.GetString("HOST"))
	if host == "" {
		host = strings.TrimSpace(v.GetString("BIND_ADDR"))
	}

	port := v.GetInt("PORT")
	if port <= 0 {
		port = 20130 // Default port (unified port)
	}

	dataDir := v.GetString("DATA_DIR")
	if dataDir == "" {
		dataDir = ResolveDataDir()
	}

	// Ensure the base data directory exists
	if err := os.MkdirAll(dataDir, constants.FilePermDir); err != nil {
		log.Warn("config", "create data dir failed", "dir", dataDir, "error", err)
	}

	// Database file: DB_PATH overrides default DATA_DIR/db/data.sqlite
	dbPath := v.GetString("DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "db", "data.sqlite")
	} else if fi, err := os.Stat(dbPath); err == nil && fi.IsDir() {
		if _, err := os.Stat(filepath.Join(dbPath, "db", "data.sqlite")); err == nil {
			dbPath = filepath.Join(dbPath, "db", "data.sqlite")
		} else if _, err := os.Stat(filepath.Join(dbPath, "data.sqlite")); err == nil {
			dbPath = filepath.Join(dbPath, "data.sqlite")
		} else if _, err := os.Stat(filepath.Join(dbPath, "9router.db")); err == nil {
			dbPath = filepath.Join(dbPath, "9router.db")
		} else {
			dbPath = filepath.Join(dbPath, "db", "data.sqlite")
		}
	}

	// INITIAL_PASSWORD has no hardcoded default — an empty value forces the
	// operator to set one explicitly rather than shipping a known password.
	initialPassword := v.GetString("INITIAL_PASSWORD")

	apiKeySecret := v.GetString("API_KEY_SECRET")
	if apiKeySecret == "" {
		apiKeySecret = "endpoint-proxy-api-key-secret"
	}

	machineIDSalt := v.GetString("MACHINE_ID_SALT")
	if machineIDSalt == "" {
		machineIDSalt = "endpoint-proxy-salt"
	}

	googleDriveBackupClientID := strings.TrimSpace(v.GetString("GOOGLE_DRIVE_BACKUP_CLIENT_ID"))
	googleDriveBackupClientSecret := strings.TrimSpace(v.GetString("GOOGLE_DRIVE_BACKUP_CLIENT_SECRET"))

	rtkEnabled := v.GetBool("RTK_ENABLED")
	cavemanEnabled := v.GetBool("CAVEMAN_ENABLED")
	ponytailEnabled := v.GetBool("PONYTAIL_ENABLED")

	return &Config{
		Host:            host,
		Port:            port,
		DatabasePath:    dbPath,
		JWTSecret:       loadJWTSecret(v, dataDir),
		InitialPassword:               initialPassword,
		APIKeySecret:                  apiKeySecret,
		MachineIDSalt:                  machineIDSalt,
		GoogleDriveBackupClientID:     googleDriveBackupClientID,
		GoogleDriveBackupClientSecret: googleDriveBackupClientSecret,
		RTKEnabled:                    rtkEnabled,
		CavemanEnabled:  cavemanEnabled,
		PonytailEnabled: ponytailEnabled,
	}
}

func loadJWTSecret(v *viper.Viper, dataDir string) string {
	var secret string
	if v != nil {
		secret = v.GetString("JWT_SECRET")
	}
	if secret == "" {
		secret = os.Getenv("JWT_SECRET")
	}
	if secret != "" {
		return secret
	}

	secretFile := filepath.Join(dataDir, "jwt-secret")
	data, err := os.ReadFile(secretFile)
	if err == nil {
		return string(data)
	}

	// Generate 32 cryptographically secure random bytes
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		log.Error("config", "crypto/rand failed to generate JWT secret; refusing to fall back to a static secret", "error", err)
		return ""
	}

	generated := hex.EncodeToString(bytes)
	if err := os.WriteFile(secretFile, []byte(generated), constants.FilePermKey); err != nil {
		log.Warn("config", "write JWT secret failed", "file", secretFile, "error", err)
	}
	return generated
}
