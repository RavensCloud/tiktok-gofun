package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	tiktok "github.com/RavensCloud/tiktok-gofun"
)

func main() {
	user := flag.String("user", "", "TikTok username to look up")
	search := flag.String("search", "", "Search videos by keyword")
	hashtag := flag.String("hashtag", "", "Search videos by hashtag")
	limit := flag.Int("limit", 10, "Max results to return")
	cookies := flag.String("cookies", "", "Path to cookies JSON file")
	proxyURL := flag.String("proxy", "", "Proxy URL (http/https/socks5)")
	flag.Parse()

	if *user == "" && *search == "" && *hashtag == "" {
		fmt.Fprintln(os.Stderr, "usage: tiktok --user <username> | --search <keyword> | --hashtag <tag>")
		os.Exit(1)
	}

	s := tiktok.New()
	defer s.Close()

	if *proxyURL != "" {
		if err := s.SetProxy(*proxyURL); err != nil {
			log.Fatalf("set proxy: %v", err)
		}
	}

	ctx := context.Background()

	// User profile lookup (pure HTTP, no browser needed).
	if *user != "" {
		author, err := s.GetUser(ctx, *user)
		if err != nil {
			log.Fatalf("get user: %v", err)
		}
		printAuthor(author)
		return
	}

	// Search and hashtag require browser + cookies.
	if *cookies != "" {
		if err := s.LoginWithCookies(*cookies); err != nil {
			log.Fatalf("login with cookies: %v", err)
		}
	} else {
		if err := s.InitBrowser(); err != nil {
			log.Fatalf("init browser: %v", err)
		}
	}

	if *search != "" {
		videos, err := s.SearchVideos(ctx, *search, *limit)
		if err != nil {
			log.Fatalf("search: %v", err)
		}
		printVideos(videos)
		return
	}

	if *hashtag != "" {
		videos, err := s.SearchByHashtag(ctx, *hashtag, *limit)
		if err != nil {
			log.Fatalf("hashtag search: %v", err)
		}
		printVideos(videos)
	}
}

func printAuthor(a tiktok.Author) {
	fmt.Printf("User:      %s\n", a.Username)
	fmt.Printf("ID:        %s\n", a.ID)
	fmt.Printf("Followers: %d\n", a.FollowerCount)
	fmt.Printf("Following: %d\n", a.FollowingCount)
	fmt.Printf("Videos:    %d\n", a.VideoCount)
	fmt.Printf("Verified:  %v\n", a.Verified)
	fmt.Printf("Bio:       %s\n", a.Bio)
}

func printVideos(videos []tiktok.Video) {
	for i, v := range videos {
		fmt.Printf("[%d] %s by @%s — %d views, %d likes (%s)\n",
			i+1, v.ID, v.Username, v.Views, v.Likes,
			v.CreatedAt.Format("2006-01-02"),
		)
		if v.Description != "" {
			fmt.Printf("    %s\n", v.Description)
		}
	}
	fmt.Printf("\nTotal: %d videos\n", len(videos))
}
