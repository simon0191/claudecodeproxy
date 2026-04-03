package claude

import "fmt"

var modelMap = map[string]string{
	"claude-sonnet-4": "sonnet",
	"claude-opus-4":   "opus",
	"claude-haiku-4":  "haiku",
}

// MapModel translates an Anthropic API model name to a Claude CLI model flag.
func MapModel(apiModel string) (string, error) {
	cliModel, ok := modelMap[apiModel]
	if !ok {
		return "", fmt.Errorf("unknown model: %s", apiModel)
	}
	return cliModel, nil
}
