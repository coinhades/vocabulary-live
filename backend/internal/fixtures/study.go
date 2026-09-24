package fixtures

import "vocabulary.live/internal/domain"

// Editorial difficulty bands give learners more reading time for harder words.
// They are guidance, not a language proficiency assessment or a speed bonus.
// Pronunciations are broad General American IPA.
func studyDetails() map[string]domain.StudyDetails {
	makeStudy := func(word, pronunciation, part, meaning, difficulty string) domain.StudyDetails {
		seconds := map[string]int{"easy": 20, "medium": 30, "hard": 40}[difficulty]
		return domain.StudyDetails{
			Timing:     domain.QuestionTiming{Difficulty: difficulty, DurationSeconds: seconds},
			Vocabulary: domain.Vocabulary{Word: word, Pronunciation: pronunciation, PartOfSpeech: part, Definition: meaning},
		}
	}
	details := map[string]domain.StudyDetails{
		"vocab-q1":  makeStudy("concise", "/kənˈsaɪs/", "adjective", "Brief and clearly expressed.", "medium"),
		"vocab-q2":  makeStudy("refine", "/rəˈfaɪn/", "verb", "To improve something through small changes.", "medium"),
		"vocab-q3":  makeStudy("reliable", "/rəˈlaɪəbəl/", "adjective", "Able to be trusted or depended on.", "easy"),
		"vocab-q4":  makeStudy("simultaneously", "/ˌsaɪməlˈteɪniəsli/", "adverb", "At the same time.", "hard"),
		"vocab-q5":  makeStudy("abundant", "/əˈbʌndənt/", "adjective", "Present in large quantities; plentiful.", "medium"),
		"vocab-q6":  makeStudy("clarify", "/ˈklɛrəˌfaɪ/", "verb", "To make something easier to understand.", "medium"),
		"vocab-q7":  makeStudy("temporary", "/ˈtɛmpəˌrɛri/", "adjective", "Lasting for a limited time.", "easy"),
		"vocab-q8":  makeStudy("feasible", "/ˈfizəbəl/", "adjective", "Practical and possible to do successfully.", "hard"),
		"travel-q1": makeStudy("itinerary", "/aɪˈtɪnəˌrɛri/", "noun", "A planned route or schedule for a journey.", "hard"),
		"travel-q2": makeStudy("destination", "/ˌdɛstəˈneɪʃən/", "noun", "The place to which someone is traveling.", "easy"),
		"travel-q3": makeStudy("departure", "/dəˈpɑrtʃər/", "noun", "The act of leaving a place.", "easy"),
		"travel-q4": makeStudy("direct", "/dəˈrɛkt/", "adjective", "Following a route without unnecessary detours.", "easy"),
		"travel-q5": makeStudy("reserve", "/rəˈzərv/", "verb", "To arrange for something to be held for future use.", "medium"),
		"travel-q6": makeStudy("pedestrian", "/pəˈdɛstriən/", "noun", "A person traveling on foot.", "hard"),
		"travel-q7": makeStudy("delayed", "/dəˈleɪd/", "adjective", "Happening later than planned or expected.", "easy"),
		"travel-q8": makeStudy("landmark", "/ˈlændˌmɑrk/", "noun", "A recognizable feature used to identify a location.", "medium"),
	}
	// Explicit editorial visibility: these targets already occur in the stem.
	// q2 and q8 of VOCAB-DEMO hide the target among options until acceptance.
	for _, id := range []string{"vocab-q1", "vocab-q3", "vocab-q4", "vocab-q5", "vocab-q6", "vocab-q7", "travel-q1", "travel-q2", "travel-q3", "travel-q4", "travel-q5", "travel-q6", "travel-q7", "travel-q8"} {
		v := details[id]
		v.PublicPronunciation = v.Vocabulary.Word
		details[id] = v
	}
	return details
}
