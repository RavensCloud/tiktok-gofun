package tiktok

import "time"

// Search API response.

type searchResponse struct {
	ItemList []rawVideo `json:"item_list"`
	HasMore  bool       `json:"has_more"`
	Cursor   int        `json:"cursor"`
}

// Challenge/hashtag API responses.

type challengeDetailResponse struct {
	ChallengeInfo rawChallengeInfo `json:"challengeInfo"`
}

type rawChallengeInfo struct {
	Challenge rawChallenge      `json:"challenge"`
	Stats     rawChallengeStats `json:"stats"`
}

type rawChallenge struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Desc  string `json:"desc"`
}

type rawChallengeStats struct {
	VideoCount int `json:"videoCount"`
	ViewCount  int `json:"viewCount"`
}

type challengeItemListResponse struct {
	ItemList []rawVideo `json:"itemList"`
	HasMore  bool       `json:"hasMore"`
	Cursor   int        `json:"cursor"`
}

// Shared raw video/author/stats structs (match TikTok JSON exactly).

type rawVideo struct {
	ID         string    `json:"id"`
	Desc       string    `json:"desc"`
	CreateTime int64     `json:"createTime"`
	Author     rawAuthor `json:"author"`
	Stats      rawStats  `json:"stats"`
}

type rawAuthor struct {
	UniqueID    string `json:"uniqueId"`
	ID          string `json:"id"`
	Nickname    string `json:"nickname"`
	AvatarThumb string `json:"avatarThumb"`
	Verified    bool   `json:"verified"`
}

type rawStats struct {
	PlayCount    int `json:"playCount"`
	DiggCount    int `json:"diggCount"`
	ShareCount   int `json:"shareCount"`
	CommentCount int `json:"commentCount"`
}

// SSR (Server-Side Rendered) data structs for __UNIVERSAL_DATA_FOR_REHYDRATION__.

type universalData struct {
	DefaultScope defaultScope `json:"__DEFAULT_SCOPE__"`
}

type defaultScope struct {
	UserDetail userDetailWrapper `json:"webapp.user-detail"`
}

type userDetailWrapper struct {
	UserInfo rawUserInfo `json:"userInfo"`
}

type rawUserInfo struct {
	User  rawUserDetail `json:"user"`
	Stats rawUserStats  `json:"stats"`
}

type rawUserDetail struct {
	ID           string `json:"id"`
	UniqueID     string `json:"uniqueId"`
	Nickname     string `json:"nickname"`
	AvatarLarger string `json:"avatarLarger"`
	Signature    string `json:"signature"`
	Verified     bool   `json:"verified"`
	SecUID       string `json:"secUid"`
}

type rawUserStats struct {
	FollowerCount  int `json:"followerCount"`
	FollowingCount int `json:"followingCount"`
	Heart          int `json:"heart"`
	HeartCount     int `json:"heartCount"`
	VideoCount     int `json:"videoCount"`
	DiggCount      int `json:"diggCount"`
}

// parseVideo converts a raw TikTok API video to the public Video type.
func parseVideo(raw rawVideo) Video {
	return Video{
		ID:          raw.ID,
		Description: raw.Desc,
		AuthorID:    raw.Author.ID,
		Username:    raw.Author.UniqueID,
		CreatedAt:   time.Unix(raw.CreateTime, 0),
		Views:       raw.Stats.PlayCount,
		Likes:       raw.Stats.DiggCount,
		Comments:    raw.Stats.CommentCount,
		Shares:      raw.Stats.ShareCount,
	}
}

// parseAuthor converts raw SSR user info to the public Author type.
func parseAuthor(raw rawUserInfo) Author {
	return Author{
		ID:             raw.User.ID,
		Username:       raw.User.UniqueID,
		FollowerCount:  raw.Stats.FollowerCount,
		FollowingCount: raw.Stats.FollowingCount,
		VideoCount:     raw.Stats.VideoCount,
		Verified:       raw.User.Verified,
		Bio:            raw.User.Signature,
		AvatarURL:      raw.User.AvatarLarger,
	}
}
