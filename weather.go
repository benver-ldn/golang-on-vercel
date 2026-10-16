package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// weatherToolSchema is the JSON Schema the model sees for the tool's arguments.
var weatherToolSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"city": map[string]any{
			"type":        "string",
			"description": "City name, for example \"Lisbon\"",
		},
	},
	"required":             []string{"city"},
	"additionalProperties": false,
}

type forecastDay struct {
	Date          string  `json:"date"`
	HighC         float64 `json:"highC"`
	LowC          float64 `json:"lowC"`
	RainChancePct float64 `json:"rainChancePct"`
}

type forecast struct {
	Location string        `json:"location"`
	Days     []forecastDay `json:"days"`
}

// getForecast fetches a 7-day forecast from Open-Meteo, which is free and
// needs no API key. Swap in your own weather provider if you have one.
func getForecast(ctx context.Context, city string) (*forecast, error) {
	var geo struct {
		Results []struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
			Country   string  `json:"country"`
		} `json:"results"`
	}
	geoURL := "https://geocoding-api.open-meteo.com/v1/search?count=1&name=" + url.QueryEscape(city)
	if err := getJSON(ctx, geoURL, &geo); err != nil {
		return nil, err
	}
	if len(geo.Results) == 0 {
		return nil, fmt.Errorf("could not find a city named %q", city)
	}
	place := geo.Results[0]

	var raw struct {
		Daily struct {
			Time       []string  `json:"time"`
			TempMax    []float64 `json:"temperature_2m_max"`
			TempMin    []float64 `json:"temperature_2m_min"`
			PrecipProb []float64 `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	fcURL := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max&timezone=auto",
		place.Latitude, place.Longitude)
	if err := getJSON(ctx, fcURL, &raw); err != nil {
		return nil, err
	}

	f := &forecast{Location: place.Name + ", " + place.Country}
	for i, date := range raw.Daily.Time {
		f.Days = append(f.Days, forecastDay{
			Date:          date,
			HighC:         raw.Daily.TempMax[i],
			LowC:          raw.Daily.TempMin[i],
			RainChancePct: raw.Daily.PrecipProb[i],
		})
	}
	return f, nil
}

func getJSON(ctx context.Context, rawURL string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
