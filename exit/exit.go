package exit

import (
	"context"
	"net/http"
	"os"
	"runtime/debug"

	"github.com/elgatito/elementum/util/event"

	"github.com/op/go-logging"
)

const (
	// ExitCodeSuccess = exit code 0
	ExitCodeSuccess = 0
	// ExitCodeError = exit code 1
	ExitCodeError = 1
	// ExitCodeRestart = exit code 5
	ExitCodeRestart = 5
)

var (
	lastExitCode = -1

	IsShared = false

	Args string
	Code int

	Closer event.Event
	Server *http.Server

	log = logging.MustGetLogger("exit")
)

func Reset() {
	lastExitCode = -1
	Args = ""
	Code = 0
}

func Exit(code int) {
	if code == ExitCodeSuccess && lastExitCode != -1 {
		code = lastExitCode
	}
	Code = code

	if Server != nil {
		Server.Shutdown(context.Background())
	}

	if IsShared {
		return
	}

	os.Exit(Code)
}

func Panic(err error) {
	PanicWithCode(err, ExitCodeError)
}

func PanicWithCode(err error, code int) {
	lastExitCode = code

	log.Errorf("Panic: %s", err)
	log.Errorf("Stacktrace: \n" + string(debug.Stack()))

	Closer.Set()

	panic(err.Error())
}
