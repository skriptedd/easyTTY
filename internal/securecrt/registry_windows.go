//go:build windows

package securecrt

import "golang.org/x/sys/windows/registry"

// ConfigPath returns SecureCRT's configured "Config Path" ("" if unset).
func ConfigPath() string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\VanDyke\SecureCRT`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Config Path")
	if err != nil {
		return ""
	}
	return v
}
