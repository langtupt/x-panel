package agentproto

import "errors"

const (
	ActionPHPManageLogs  = "php.manage.logs"
	ActionPHPManageList  = "php.manage.list"
	ActionPHPManageApply = "php.manage.apply"
)

type PHPManageParams struct {
	Operation string            `json:"operation,omitempty"`
	Version   string            `json:"version"`
	Module    string            `json:"module,omitempty"`
	Remove    bool              `json:"remove,omitempty"`
	Settings  map[string]string `json:"settings,omitempty"`
	Reset     []string          `json:"reset,omitempty"`
}
type PHPModule struct {
	Upgradable bool     `json:"upgradable"`
	Enabled    bool     `json:"enabled"`
	Loaded     bool     `json:"loaded"`
	Builtin    bool     `json:"builtin"`
	Candidate  string   `json:"candidate"`
	Modules    []string `json:"modules"`
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Installed  bool     `json:"installed"`
	Removable  bool     `json:"removable"`
}
type PHPSetting struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Kind   string `json:"kind"`
	Custom bool   `json:"custom"`
}
type PHPManagement struct {
	Modules  []PHPModule  `json:"modules"`
	Settings []PHPSetting `json:"settings"`
}

func (p PHPManageParams) Validate() error {
	if !ValidPHPVersion(p.Version) {
		return errors.New("invalid PHP version")
	}
	hasSettings := len(p.Settings) > 0 || len(p.Reset) > 0
	if p.Operation == "refresh" {
		if p.Module != "" || p.Remove || hasSettings {
			return errors.New("mixed refresh operation")
		}
		return nil
	}
	if p.Module == "" {
		if p.Operation != "" || p.Remove || !hasSettings {
			return errors.New("empty or invalid settings operation")
		}
		return nil
	}
	if hasSettings {
		return errors.New("mixed module and settings operation")
	}
	switch p.Operation {
	case "":
		return nil
	case "enable", "disable", "update":
		if !p.Remove {
			return nil
		}
	}
	return errors.New("invalid module operation")
}
