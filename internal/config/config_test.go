package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"NPL_SERVER_URL": "http://tower.local:8080/", "NPL_DEVICE_TOKEN": "tok"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.ServerURL != "http://tower.local:8080" || c.AudioDevice != "default" || c.SampleRate != 44100 || c.ClipSeconds != 10 ||
		c.SearchInterval != 30*time.Second || c.TrackInterval != 3*time.Minute || c.HeartbeatInterval != 30*time.Second || c.SilenceDBFS != -45 {
		t.Fatalf("%+v", c)
	}
}

func TestEveryProblemIsReported(t *testing.T) {
	_, err := Load(env(map[string]string{"NPL_SAMPLE_RATE": "abc", "NPL_SEARCH_INTERVAL": "1s", "NPL_SILENCE_DBFS": "5", "NPL_LOG_LEVEL": "loud"}))
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, name := range []string{"NPL_SERVER_URL", "NPL_API_KEY", "NPL_SAMPLE_RATE", "NPL_SEARCH_INTERVAL", "NPL_SILENCE_DBFS", "NPL_LOG_LEVEL"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("missing %s in:\n%v", name, err)
		}
	}
}

func TestBadURL(t *testing.T) {
	_, err := Load(env(map[string]string{"NPL_SERVER_URL": "tower.local", "NPL_DEVICE_TOKEN": "t"}))
	if err == nil || !strings.Contains(err.Error(), "NPL_SERVER_URL") {
		t.Fatalf("%v", err)
	}
}

func TestLogValueHidesToken(t *testing.T) {
	c := Config{DeviceToken: "super-secret"}
	if strings.Contains(c.LogValue().String(), "super-secret") {
		t.Fatal("token leaked")
	}
}

func TestAPIKeyBeatsTheOldDeviceTokenName(t *testing.T) {
	both, err := Load(env(map[string]string{"NPL_SERVER_URL": "http://tower.local:8080", "NPL_API_KEY": "npk_new", "NPL_DEVICE_TOKEN": "old"}))
	if err != nil || both.DeviceToken != "npk_new" {
		t.Fatalf("with both set the API key should win: %+v %v", both, err)
	}
	old, err := Load(env(map[string]string{"NPL_SERVER_URL": "http://tower.local:8080", "NPL_DEVICE_TOKEN": "old"}))
	if err != nil || old.DeviceToken != "old" {
		t.Fatalf("installs from before API keys should keep working: %+v %v", old, err)
	}
}
