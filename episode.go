package main

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// EpisodeInfo is what we could work out about which episode(s) a release has
type EpisodeInfo struct {
	Episode  int  // single episode number, 0 if unknown
	Batch    bool // a multi-episode release
	From, To int  // episode range for batches, 0 if unknown
}

// Contains reports whether the release (probably) includes episode ep
func (e EpisodeInfo) Contains(ep int) bool {
	if e.Episode != 0 {
		return e.Episode == ep
	}
	if e.Batch && e.From > 0 && e.To >= e.From {
		return ep >= e.From && ep <= e.To
	}
	return false
}

// Label is a short badge like "EP 05" or "01-12"
func (e EpisodeInfo) Label() string {
	switch {
	case e.Episode != 0:
		return "EP " + pad2(e.Episode)
	case e.Batch && e.From > 0:
		return pad2(e.From) + "-" + pad2(e.To)
	case e.Batch:
		return "Batch"
	}
	return ""
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

var (
	reBrackets = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	// Numbers that are never episode numbers: resolutions, codecs, years,
	// audio channels, bit depth, seasons/parts, versions, CRCs
	reNoise  = regexp.MustCompile(`(?i)\b\d{3,4}p\b|\b\d{3,4}x\d{3,4}\b|\b[xh]\.?26[45]\b|\b(?:10|8)[ -]?bits?\b|\b(?:19|20)\d{2}\b|\b\d\.\d\b|\b(?:season|part|cour|vol\.?)\s*\d+\b|\b\d+(?:st|nd|rd|th)\s+season\b|\bs\d{1,2}\b|\b[a-f0-9]{8}\b|\bav1\b|\bmp4\b|\bmkv\b`)
	reSxxExx = regexp.MustCompile(`(?i)\bS\d{1,2}\s*E(\d{1,4})\b`)
	// "01-12", "01~12", "01 ~ 12", "1 to 12"; "8 - 12" is left alone since
	// that's how single episodes are usually written ("Kaiju No. 8 - 12")
	reRange   = regexp.MustCompile(`(?i)\b(\d{1,4})(?:-|\s*~\s*|\s+to\s+)(\d{1,4})\b`)
	reEpWord  = regexp.MustCompile(`(?i)\b(?:ep|eps|episode|e)\.?\s*(\d{1,4})(?:v\d)?\b`)
	reDashNum = regexp.MustCompile(`\s-\s+(\d{1,4})(?:v\d)?(?:\s|$)`)
	reBareNum = regexp.MustCompile(`\s(\d{1,4})(?:v\d)?\s*$`)
	reBatch   = regexp.MustCompile(`(?i)\b(?:batch|complete|bd\s*box|全集)\b`)
)

// parseEpisode guesses the episode (or episode range) from a release title
// or file name, e.g. "[SubsPlease] Dandadan - 05 (1080p) [ABCD1234].mkv"
func parseEpisode(title string) EpisodeInfo {
	var info EpisodeInfo

	// SxxExx survives the noise stripping, so check it first
	if m := reSxxExx.FindStringSubmatch(title); m != nil {
		info.Episode, _ = strconv.Atoi(m[1])
		return info
	}

	batchWord := reBatch.MatchString(title)

	s := title
	// Only strip real extensions: path.Ext("Kaiju No. 8 - 12") is ". 8 - 12"
	switch strings.ToLower(path.Ext(s)) {
	case ".mkv", ".mp4", ".avi", ".webm", ".m4v", ".ts", ".mov", ".wmv":
		s = strings.TrimSuffix(s, path.Ext(s))
	}
	s = reBrackets.ReplaceAllString(s, " ")
	s = strings.NewReplacer("(", " ", ")", " ", "_", " ").Replace(s)
	s = reNoise.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, ".", " ")
	s = " " + strings.Join(strings.Fields(s), " ") + " "

	if m := reRange.FindStringSubmatch(s); m != nil {
		from, _ := strconv.Atoi(m[1])
		to, _ := strconv.Atoi(m[2])
		if from < to && to-from < 2000 {
			return EpisodeInfo{Batch: true, From: from, To: to}
		}
	}

	if batchWord {
		return EpisodeInfo{Batch: true}
	}

	for _, re := range []*regexp.Regexp{reEpWord, reDashNum, reBareNum} {
		if m := re.FindStringSubmatch(s); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				info.Episode = n
				return info
			}
		}
	}
	return info
}

// seedersUnknown is what seedersOf returns for sites that don't report seeders
const seedersUnknown = -1

// seedersOf normalises the Seeders field, which is a number from AnimeTosho's
// JSON, an int from nyaa and missing for SubsPlease/TokyoTosho
func seedersOf(t Torrent) int {
	switch v := t.Seeders.(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return seedersUnknown
}
