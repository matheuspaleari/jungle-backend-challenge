package main

import (
	"go.uber.org/fx"

	platformapp "github.com/matheuspaleari/jungle-backend-challenge/internal/platform/app"
)

func main() {
	fx.New(
		platformapp.Module,
	).Run()
}
