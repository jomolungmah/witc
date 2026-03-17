package formatter

import (
	"encoding/json"
)

// JSON formats the summary as JSON.
func JSON(sum *Summary) (string, error) {
	data, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
