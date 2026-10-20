// Package phpconfig defines the allowed PHP directives; never accepts arbitrary ini code.
package phpconfig

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var Kinds = map[string]string{
	"allow_url_fopen": "bool", "allow_url_include": "bool", "display_errors": "bool", "display_startup_errors": "bool", "log_errors": "bool", "html_errors": "bool", "expose_php": "bool", "file_uploads": "bool", "short_open_tag": "bool", "ignore_user_abort": "bool", "implicit_flush": "bool", "output_buffering": "int", "max_execution_time": "int", "max_input_time": "signed", "max_input_vars": "int", "max_file_uploads": "int", "default_socket_timeout": "int", "memory_limit": "size", "post_max_size": "size", "upload_max_filesize": "size", "date.timezone": "timezone", "default_charset": "text", "error_reporting": "int", "serialize_precision": "signed", "precision": "int", "session.gc_maxlifetime": "int", "session.cookie_lifetime": "int", "session.cookie_secure": "bool", "session.cookie_httponly": "bool", "session.use_strict_mode": "bool", "session.use_only_cookies": "bool", "session.cookie_samesite": "samesite", "opcache.enable": "bool", "opcache.memory_consumption": "int", "opcache.max_accelerated_files": "int", "opcache.validate_timestamps": "bool", "opcache.revalidate_freq": "int", "realpath_cache_size": "size", "realpath_cache_ttl": "int", "zlib.output_compression": "bool",
}

func ValidateSetting(name, value string) error {
	kind := Kinds[name]
	if kind == "" {
		return fmt.Errorf("unsupported directive: %s", name)
	}
	if len(value) > 200 || strings.ContainsAny(value, "\r\n\x00\"\\;$") {
		return errors.New("invalid directive value")
	}
	switch kind {
	case "bool":
		if value != "0" && value != "1" {
			return errors.New("expected 0 or 1")
		}
	case "int", "signed":
		n, err := strconv.ParseInt(value, 10, 32)
		if err != nil || n < -1 || (kind == "int" && n < 0) {
			return errors.New("invalid integer")
		}
	case "size":
		if !regexp.MustCompile(`^(?:-1|[0-9]+[KMGkmg]?)$`).MatchString(value) {
			return errors.New("invalid size (use e.g. 256M)")
		}
	case "timezone":
		if _, err := time.LoadLocation(value); err != nil {
			return errors.New("invalid timezone")
		}
	case "samesite":
		if value != "" && value != "Lax" && value != "Strict" && value != "None" {
			return errors.New("invalid SameSite value")
		}
	}
	return nil
}

// Site is persisted per website. Zero FPM values use safe defaults from the hosting plan.
type Site struct {
	Settings       map[string]string `json:"settings"`
	Workers        int               `json:"workers"`
	RequestTimeout int               `json:"request_timeout"`
	SlowTimeout    int               `json:"slow_timeout"`
}

func (s Site) Validate() error {
	if len(s.Settings) > len(Kinds) {
		return errors.New("too many directives")
	}
	for k, v := range s.Settings {
		if !SiteAllowed(k) {
			return fmt.Errorf("directive requires version-wide configuration: %s", k)
		}
		if err := ValidateSetting(k, v); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	if s.Workers < 0 || s.Workers > 50 || s.RequestTimeout < 0 || s.RequestTimeout > 3600 || s.SlowTimeout < 0 || s.SlowTimeout > 3600 {
		return errors.New("FPM limits out of range")
	}
	if s.SlowTimeout > 0 && s.RequestTimeout > 0 && s.SlowTimeout >= s.RequestTimeout {
		return errors.New("slow timeout must be below request timeout")
	}
	return nil
}

func SiteAllowed(name string) bool {
	if Kinds[name] == "" {
		return false
	}
	switch name {
	case "opcache.memory_consumption", "opcache.max_accelerated_files", "realpath_cache_size", "realpath_cache_ttl":
		return false
	}
	return true
}
