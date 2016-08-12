package main

import (
	"encoding/json"
	"os"
)

type Configuration struct {
	Scaling float64
}

var conf Configuration

func adjustConfiguration() {
	if conf.Scaling < 0.2 {
		conf.Scaling = 1.0
	}
}

func loadConfiguration() {
	defer adjustConfiguration()
	fh, err := os.Open(os.ExpandEnv("$HOME/.config/gdlv"))
	if err != nil {
		return
	}
	defer fh.Close()
	json.NewDecoder(fh).Decode(&conf)
}

func saveConfiguration() {
	fh, err := os.Create(os.ExpandEnv("$HOME/.config/gdlv"))
	if err != nil {
		return
	}
	defer fh.Close()
	json.NewEncoder(fh).Encode(&conf)
}
