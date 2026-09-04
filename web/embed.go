package web

import "embed"

// Files sadrži ugrađene predloške i statičke resurse (CSS, JS)
//
//go:embed static/* templates/*
var Files embed.FS
