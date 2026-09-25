// ai-smoke is opt-in only. It is not in the app image or normal verification.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"vocabulary.live/internal/learning"
)

func main() {
	allow := flag.Bool("allow-paid-api", false, "Explicitly authorize two small billable OpenAI requests")
	flag.Parse()
	c := learning.FromEnv()
	if !*allow || c.Key == "" {
		fmt.Fprintln(os.Stderr, "No provider calls made. Set OPENAI_API_KEY and pass --allow-paid-api to opt in.")
		os.Exit(2)
	}
	p := learning.NewOpenAI(c)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	b, err := p.Text(ctx, learning.TextRequest{Action: "example", Instructions: "Return one short English sentence using concise to mean brief and clear. No URLs or markup.", Input: map[string]string{"word": "concise", "meaning": "brief and clear"}, Schema: map[string]any{"type": "object", "properties": map[string]any{"sentence": map[string]any{"type": "string", "maxLength": 240}}, "required": []string{"sentence"}, "additionalProperties": false}, Tokens: 1200})
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Text smoke failed:", err)
		os.Exit(1)
	}
	var result struct {
		Sentence string `json:"sentence"`
	}
	if !learning.Decode(b, &result) || !learning.Plain(result.Sentence, 240, false) {
		fmt.Fprintln(os.Stderr, "Text smoke failed validation")
		os.Exit(1)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	audio, err := p.Speech(ctx, learning.SpeechRequest{Text: "concise", PartOfSpeech: "adjective", Sense: "brief and clear"})
	cancel()
	if err != nil || !learning.ValidMP3(audio) {
		fmt.Fprintln(os.Stderr, "Speech smoke failed validation or provider request")
		os.Exit(1)
	}
	fmt.Printf("Text and bounded MP3 responses received (%d audio bytes). No files saved.\n", len(audio))
	fmt.Println("Generated example for human review:", result.Sentence)
	fmt.Println("This checks provider wiring only. Listen and review teaching quality in the app separately.")
}
