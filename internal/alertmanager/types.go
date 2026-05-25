package alertmanager

import "time"

type Alert struct {
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
	Status       AlertStatus       `json:"status"`
}

type AlertStatus struct {
	State      string   `json:"state"`
	SilencedBy []string `json:"silencedBy"`
	InhibitedBy []string `json:"inhibitedBy"`
}
