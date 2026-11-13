//go:build !windows

package securecrt

// ConfigPath: SecureCRT's registry setting only exists on Windows.
func ConfigPath() string { return "" }
