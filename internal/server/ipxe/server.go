package ipxe

import (
	"fmt"
	"net/http"
)

const dummyScript = `#!ipxe
echo Tart placeholder boot script
sleep 3
`

func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ipxe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := fmt.Fprint(w, dummyScript); err != nil {
			http.Error(w, "failed to write response", http.StatusInternalServerError)
		}
	})

	return mux
}
