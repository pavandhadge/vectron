package runtimeutil

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// LoadServiceEnv loads environment variables for a service from a local env file.
// Order of precedence:
// 1) <SERVICE>_ENV_FILE (explicit path)
// 2) VECTRON_ENV_FILE (explicit path)
// 3) .env.<service>, <service>.env, env/<service>.env (searched in cwd and executable dir)
// Existing environment variables are not overridden unless VECTRON_ENV_OVERRIDE=1.
func LoadServiceEnv(service string) {
	override := os.Getenv("VECTRON_ENV_OVERRIDE") == "1"
	serviceKey := strings.ToUpper(sanitizeEnvKey(service))

	candidates := []string{
		".env." + service,
		service + ".env",
		filepath.Join("env", service+".env"),
	}

	roots := []string{""}
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd)
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe))
	}

	for _, root := range roots {
		for _, rel := range candidates {
			path := rel
			if root != "" {
				path = filepath.Join(root, rel)
			}
			if _, err := os.Stat(path); err == nil {
				loadEnvFile(path, override, service)
				return
			}
		}
	}
	if path := os.Getenv(serviceKey + "_ENV_FILE"); path != "" {
		loadEnvFile(path, override, service)
		return
	}
	if path := os.Getenv("VECTRON_ENV_FILE"); path != "" {
		loadEnvFile(path, override, service)
		return
	}
}

func sanitizeEnvKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func loadEnvFile(path string, override bool, service string) {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("%s: failed to open env file %s: %v", service, path, err)
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	loaded := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := splitEnvLine(line)
		if !ok {
			continue
		}
		if !override {
			if _, exists := os.LookupEnv(key); exists {
				continue
			}
		}
		_ = os.Setenv(key, val)
		loaded++
	}
	if err := scanner.Err(); err != nil {
		log.Printf("%s: error reading env file %s: %v", service, path, err)
		return
	}
	if loaded > 0 {
		log.Printf("%s: loaded %d env vars from %s", service, loaded, path)
	} else {
		log.Printf("%s: no env vars loaded from %s", service, path)
	}
}

func splitEnvLine(line string) (string, string, bool) {
	idx := strings.Index(line, "=")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	if key == "" {
		return "", "", false
	}
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return key, val, true
}
