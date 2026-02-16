package tiktok

import "time"

// Video represents a TikTok video with its engagement metrics.
type Video struct {
	ID          string
	Description string
	AuthorID    string
	Username    string
	CreatedAt   time.Time
	Views       int
	Likes       int
	Comments    int
	Shares      int
}

// Author represents a TikTok user profile with their stats.
type Author struct {
	ID             string
	Username       string
	Nickname       string // Display name (bot detection: random/empty patterns).
	FollowerCount  int
	FollowingCount int
	VideoCount     int
	HeartCount     int // Total likes received across all videos.
	DiggCount      int // Total likes given by the user.
	Verified       bool
	Bio            string
	AvatarURL      string
}
