package providers

import "os"

// osGetenv is the environment seam; tests swap it to control env-referenced
// credentials without polluting the process environment.
var osGetenv = os.Getenv
