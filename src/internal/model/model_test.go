package model

import "testing"

func TestConfigValidation(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Config){func(c *Config) { c.Timezone = "invalid" }, func(c *Config) { c.Widgets[1].ID = c.Widgets[0].ID }, func(c *Config) { c.Widgets[1].Y = 0 }, func(c *Config) { c.Widgets[0].Latitude = 91 }, func(c *Config) { c.Widgets[1].W = 3 }, func(c *Config) { c.Widgets[0].Type = "script" }} {
		c := DefaultConfig()
		change(&c)
		if c.Validate() == nil {
			t.Fatal("invalid config accepted")
		}
	}
}
