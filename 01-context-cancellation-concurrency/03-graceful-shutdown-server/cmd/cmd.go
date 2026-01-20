package main

import gracefulshutdownserver "graceful-shutdown-serve"

func main() {
	app := gracefulshutdownserver.InitApplication("localhost:8080", "localhost:8080")
	app.Start()
}
