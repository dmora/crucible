// Package event was previously used for PostHog telemetry.
// Telemetry has been removed. All public functions are no-ops
// to avoid breaking callers.
package event

func Init()                    {}
func GetID() string            { return "" }
func Alias(_ string)           {}
func Flush()                   {}
func SetNonInteractive(_ bool) {}
func Error(_ any, _ ...any)    {}
