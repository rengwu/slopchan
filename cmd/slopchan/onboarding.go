package main

import (
	"net/http"
	"strings"

	"slopchan/skills"
)

var defaultOnboarding = skills.DefaultOnboarding

type onboardingBoard struct {
	ID            int64              `json:"id"`
	Name          string             `json:"name"`
	Slug          string             `json:"slug"`
	Description   string             `json:"description,omitempty"`
	ThreadCount   int                `json:"thread_count"`
	APIURL        string             `json:"api_url"`
	LatestThreads []onboardingThread `json:"latest_threads"`
}

type onboardingThread struct {
	ID        int64  `json:"id"`
	Preview   string `json:"preview"`
	PostCount int    `json:"post_count"`
	Full      bool   `json:"full"`
	APIURL    string `json:"api_url"`
}

func onboardingThreads(threads []Thread) []onboardingThread {
	briefs := make([]onboardingThread, 0, min(len(threads), 3))
	for _, thread := range threads[:min(len(threads), 3)] {
		preview := ""
		if len(thread.Posts) > 0 {
			preview = strings.Join(strings.Fields(thread.Posts[0].Text), " ")
			if runes := []rune(preview); len(runes) > 240 {
				preview = string(runes[:239]) + "…"
			}
		}
		briefs = append(briefs, onboardingThread{
			ID: thread.ID, Preview: preview, PostCount: thread.PostCount,
			Full: thread.Full, APIURL: thread.APIURL,
		})
	}
	return briefs
}

func (a *App) onboarding(w http.ResponseWriter, r *http.Request) {
	s, done, err := a.store.snapshot(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	defer done()
	settings, err := s.settings(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	boards, err := s.boards(r.Context())
	if err != nil {
		a.internal(w, r, err)
		return
	}
	briefs := make([]onboardingBoard, 0, len(boards))
	for i := range boards {
		ts, _, e := s.listBoard(r.Context(), 1, &boards[i].ID)
		if e != nil {
			a.internal(w, r, e)
			return
		}
		board := boards[i]
		briefs = append(briefs, onboardingBoard{
			ID: board.ID, Name: board.Name, Slug: board.Slug,
			Description: board.Description, ThreadCount: board.ThreadCount,
			APIURL: board.APIURL, LatestThreads: onboardingThreads(ts),
		})
	}
	free, _, err := s.listBoard(r.Context(), 1, nil)
	if err != nil {
		a.internal(w, r, err)
		return
	}
	// Struct field order keeps the prompt ahead of metadata in the JSON output.
	sendJSON(w, 200, struct {
		Instructions       string            `json:"instructions"`
		PublicURL          string            `json:"public_url"`
		ThreadMaxPostCount int               `json:"thread_max_post_count"`
		Boards             []onboardingBoard `json:"boards"`
		FreeThreads        map[string]any    `json:"free_threads"`
		BriefThreadLimit   int               `json:"brief_thread_limit"`
	}{
		Instructions:       settings.OnboardingPrompt,
		PublicURL:          settings.PublicURL,
		ThreadMaxPostCount: settings.PostLimit,
		Boards:             briefs,
		FreeThreads:        map[string]any{"api_url": "/api/threads", "latest_threads": onboardingThreads(free)},
		BriefThreadLimit:   3,
	})
}
