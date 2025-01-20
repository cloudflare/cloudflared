package hello

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/cloudflare/cloudflared/tlsconfig"
)

const (
	UptimeRoute    = "/uptime"
	WSRoute        = "/ws"
	SSERoute       = "/sse"
	HealthRoute    = "/_health"
	defaultSSEFreq = time.Second * 10
)

type templateData struct {
	ServerName string
	Request    *http.Request
	Body       string
}

type OriginUpTime struct {
	StartTime time.Time `json:"startTime"`
	UpTime    string    `json:"uptime"`
}

const defaultServerName = "the Cloudflare Tunnel test server"
const indexTemplate = `
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta http-equiv="X-UA-Compatible" content="IE=Edge">
    <title>
      Cloudflare Tunnel Connection
    </title>
    <meta name="author" content="">
    <meta name="description" content="Cloudflare Tunnel Connection">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
      html{line-height:1.15;-ms-text-size-adjust:100%;-webkit-text-size-adjust:100%}body{margin:0}section{display:block}h1{font-size:2em;margin:.67em 0}a{background-color:transparent;-webkit-text-decoration-skip:objects}/* 1 */::-webkit-file-upload-button{-webkit-appearance:button;font:inherit}/* 1 */a,body,dd,div,dl,dt,h1,h4,html,p,section{box-sizing:border-box}.bt{border-top-style:solid;border-top-width:1px}.bl{border-left-style:solid;border-left-width:1px}.b--orange{border-color:#f38020}.br1{border-radius:.125rem}.bw2{border-width:.25rem}.dib{display:inline-block}.sans-serif{font-family:open sans,-apple-system,BlinkMacSystemFont,avenir next,avenir,helvetica neue,helvetica,ubuntu,roboto,noto,segoe ui,arial,sans-serif}.overflow-x-auto{overflow-x:auto}.code{font-family:Consolas,monaco,monospace}.b{font-weight:700}.fw3{font-weight:300}.fw4{font-weight:400}.fw5{font-weight:500}.fw6{font-weight:600}.lh-copy{line-height:1.5}.link{text-decoration:none}.link,.link:active,.link:focus,.link:hover,.link:link,.link:visited{transition:color .15s ease-in}.link:focus{outline:1px dotted currentColor}.mw-100{max-width:100%}.mw4{max-width:8rem}.mw7{max-width:48rem}.bg-light-gray{background-color:#f7f7f7}.link-hover:hover{background-color:#1f679e}.white{color:#fff}.bg-white{background-color:#fff}.bg-blue{background-color:#408bc9}.pb2{padding-bottom:.5rem}.pb6{padding-bottom:8rem}.pt3{padding-top:1rem}.pt5{padding-top:4rem}.pv2{padding-top:.5rem;padding-bottom:.5rem}.ph3{padding-left:1rem;padding-right:1rem}.ph4{padding-left:2rem;padding-right:2rem}.ml0{margin-left:0}.mb1{margin-bottom:.25rem}.mb2{margin-bottom:.5rem}.mb3{margin-bottom:1rem}.mt5{margin-top:4rem}.ttu{text-transform:uppercase}.f4{font-size:1.25rem}.f5{font-size:1rem}.f6{font-size:.875rem}.f7{font-size:.75rem}.measure{max-width:30em}.center{margin-left:auto}.center{margin-right:auto}@media screen and (min-width:30em){.f2-ns{font-size:2.25rem}}@media screen and (min-width:30em) and (max-width:60em){.f5-m{font-size:1rem}}@media screen and (min-width:60em){.f4-l{font-size:1.25rem}}
    .st0{fill:#FFF}.st1{fill:#f48120}.st2{fill:#faad3f}.st3{fill:#404041}
    </style>
  </head>
  <body class="sans-serif black">
    <div class="bt bw2 b--orange bg-white pb6">
      <div class="mw7 center ph4 pt3">
        <svg id="Layer_2" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 109 40.5" class="mw4">
          <path class="st0" d="M98.6 14.2L93 12.9l-1-.4-25.7.2v12.4l32.3.1z"/>
          <path class="st1" d="M88.1 24c.3-1 .2-2-.3-2.6-.5-.6-1.2-1-2.1-1.1l-17.4-.2c-.1 0-.2-.1-.3-.1-.1-.1-.1-.2 0-.3.1-.2.2-.3.4-.3l17.5-.2c2.1-.1 4.3-1.8 5.1-3.8l1-2.6c0-.1.1-.2 0-.3-1.1-5.1-5.7-8.9-11.1-8.9-5 0-9.3 3.2-10.8 7.7-1-.7-2.2-1.1-3.6-1-2.4.2-4.3 2.2-4.6 4.6-.1.6 0 1.2.1 1.8-3.9.1-7.1 3.3-7.1 7.3 0 .4 0 .7.1 1.1 0 .2.2.3.3.3h32.1c.2 0 .4-.1.4-.3l.3-1.1z"/>
          <path class="st2" d="M93.6 12.8h-.5c-.1 0-.2.1-.3.2l-.7 2.4c-.3 1-.2 2 .3 2.6.5.6 1.2 1 2.1 1.1l3.7.2c.1 0 .2.1.3.1.1.1.1.2 0 .3-.1.2-.2.3-.4.3l-3.8.2c-2.1.1-4.3 1.8-5.1 3.8l-.2.9c-.1.1 0 .3.2.3h13.2c.2 0 .3-.1.3-.3.2-.8.4-1.7.4-2.6 0-5.2-4.3-9.5-9.5-9.5"/>
          <path class="st3" d="M104.4 30.8c-.5 0-.9-.4-.9-.9s.4-.9.9-.9.9.4.9.9-.4.9-.9.9m0-1.6c-.4 0-.7.3-.7.7 0 .4.3.7.7.7.4 0 .7-.3.7-.7 0-.4-.3-.7-.7-.7m.4 1.2h-.2l-.2-.3h-.2v.3h-.2v-.9h.5c.2 0 .3.1.3.3 0 .1-.1.2-.2.3l.2.3zm-.3-.5c.1 0 .1 0 .1-.1s-.1-.1-.1-.1h-.3v.3h.3zM14.8 29H17v6h3.8v1.9h-6zM23.1 32.9c0-2.3 1.8-4.1 4.3-4.1s4.2 1.8 4.2 4.1-1.8 4.1-4.3 4.1c-2.4 0-4.2-1.8-4.2-4.1m6.3 0c0-1.2-.8-2.2-2-2.2s-2 1-2 2.1.8 2.1 2 2.1c1.2.2 2-.8 2-2M34.3 33.4V29h2.2v4.4c0 1.1.6 1.7 1.5 1.7s1.5-.5 1.5-1.6V29h2.2v4.4c0 2.6-1.5 3.7-3.7 3.7-2.3-.1-3.7-1.2-3.7-3.7M45 29h3.1c2.8 0 4.5 1.6 4.5 3.9s-1.7 4-4.5 4h-3V29zm3.1 5.9c1.3 0 2.2-.7 2.2-2s-.9-2-2.2-2h-.9v4h.9zM55.7 29H62v1.9h-4.1v1.3h3.7V34h-3.7v2.9h-2.2zM65.1 29h2.2v6h3.8v1.9h-6zM76.8 28.9H79l3.4 8H80l-.6-1.4h-3.1l-.6 1.4h-2.3l3.4-8zm2 4.9l-.9-2.2-.9 2.2h1.8zM85.2 29h3.7c1.2 0 2 .3 2.6.9.5.5.7 1.1.7 1.8 0 1.2-.6 2-1.6 2.4l1.9 2.8H90l-1.6-2.4h-1v2.4h-2.2V29zm3.6 3.8c.7 0 1.2-.4 1.2-.9 0-.6-.5-.9-1.2-.9h-1.4v1.9h1.4zM95.3 29h6.4v1.8h-4.2V32h3.8v1.8h-3.8V35h4.3v1.9h-6.5zM10 33.9c-.3.7-1 1.2-1.8 1.2-1.2 0-2-1-2-2.1s.8-2.1 2-2.1c.9 0 1.6.6 1.9 1.3h2.3c-.4-1.9-2-3.3-4.2-3.3-2.4 0-4.3 1.8-4.3 4.1s1.8 4.1 4.2 4.1c2.1 0 3.7-1.4 4.2-3.2H10z"/>
        </svg>
        <h1 class="f4 f2-ns mt5 fw5">Congrats! You created a tunnel!</h1>
        <p class="f6 f5-m f4-l measure lh-copy fw3">
          Cloudflare Tunnel exposes locally running applications to the internet by
          running an encrypted, virtual tunnel from your laptop or server to
          Cloudflare's edge network.
        </p>
        <p class="b f5 mt5 fw6">Ready for the next step?</p>
        <a
          class="fw6 link white bg-blue ph4 pv2 br1 dib f5 link-hover"
          style="border-bottom: 1px solid #1f679e"
          href="https://developers.cloudflare.com/cloudflare-one/connections/connect-apps">
          Get started here
        </a>
       <section>
          <h4 class="f6 fw4 pt5 mb2">Request</h4>
          <dl class="bl bw2 b--orange ph3 pt3 pb2 bg-light-gray f7 code overflow-x-auto mw-100">
						<dd class="ml0 mb3 f5">Method: {{.Request.Method}}</dd>
						<dd class="ml0 mb3 f5">Protocol: {{.Request.Proto}}</dd>
						<dd class="ml0 mb3 f5">Request URL: {{.Request.URL}}</dd>
						<dd class="ml0 mb3 f5">Transfer encoding: {{.Request.TransferEncoding}}</dd>
						<dd class="ml0 mb3 f5">Host: {{.Request.Host}}</dd>
						<dd class="ml0 mb3 f5">Remote address: {{.Request.RemoteAddr}}</dd>
						<dd class="ml0 mb3 f5">Request URI: {{.Request.RequestURI}}</dd>
{{range $key, $value := .Request.Header}}
						<dd class="ml0 mb3 f5">Header: {{$key}}, Value: {{$value}}</dd>
{{end}}
						<dd class="ml0 mb3 f5">Body: {{.Body}}</dd>
					</dl>
        </section>
     </div>
    </div>
  </body>
</html>
`

func StartHelloWorldServer(log *zerolog.Logger, listener net.Listener, shutdownC <-chan struct{}) error {
	log.Info().Msgf("Starting Hello World server at %s", listener.Addr())
	serverName := defaultServerName
	if hostname, err := os.Hostname(); err == nil {
		serverName = hostname
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	muxer := http.NewServeMux()
	muxer.HandleFunc(UptimeRoute, uptimeHandler(time.Now()))
	muxer.HandleFunc(WSRoute, websocketHandler(log, upgrader))
	muxer.HandleFunc(SSERoute, sseHandler(log))
	muxer.HandleFunc(HealthRoute, healthHandler())
	muxer.HandleFunc("/", rootHandler(serverName))
	httpServer := &http.Server{Addr: listener.Addr().String(), Handler: muxer}
	go func() {
		<-shutdownC
		_ = httpServer.Close()
	}()

	err := httpServer.Serve(listener)
	return err
}

func CreateTLSListener(address string) (net.Listener, error) {
	certificate, err := tlsconfig.GetHelloCertificate()
	if err != nil {
		return nil, err
	}

	// If the port in address is empty, a port number is automatically chosen
	listener, err := tls.Listen(
		"tcp",
		address,
		&tls.Config{Certificates: []tls.Certificate{certificate}})

	return listener, err
}

func uptimeHandler(startTime time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Note that if autoupdate is enabled, the uptime is reset when a new client
		// release is available
		resp := &OriginUpTime{StartTime: startTime, UpTime: time.Now().Sub(startTime).String()}
		respJson, err := json.Marshal(resp)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(respJson)
		}
	}
}

// This handler will echo message
func websocketHandler(log *zerolog.Logger, upgrader websocket.Upgrader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// This addresses the issue of r.Host includes port but origin header doesn't
		host, _, err := net.SplitHostPort(r.Host)
		if err == nil {
			r.Host = host
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Err(err).Msg("failed to upgrade to websocket connection")
			return
		}
		defer conn.Close()
		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				log.Err(err).Msg("websocket read message error")
				break
			}

			if err := conn.WriteMessage(mt, message); err != nil {
				log.Err(err).Msg("websocket write message error")
				break
			}
		}
	}
}

func sseHandler(log *zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		flusher, ok := w.(http.Flusher)
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			log.Error().Msgf("Can't support SSE. ResponseWriter %T doesn't implement http.Flusher interface", w)
			return
		}

		freq := defaultSSEFreq
		if requestedFreq := r.URL.Query()["freq"]; len(requestedFreq) > 0 {
			parsedFreq, err := time.ParseDuration(requestedFreq[0])
			if err == nil {
				freq = parsedFreq
			}
		}
		log.Info().Msgf("Server Sent Events every %s", freq)
		ticker := time.NewTicker(freq)
		counter := 0
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
			}
			_, err := fmt.Fprintf(w, "%d\n\n", counter)
			if err != nil {
				return
			}
			flusher.Flush()
			counter++
		}
	}
}

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}
}

func rootHandler(serverName string) http.HandlerFunc {
	responseTemplate := template.Must(template.New("index").Parse(indexTemplate))
	return func(w http.ResponseWriter, r *http.Request) {
		var buffer bytes.Buffer
		var body string
		rawBody, err := io.ReadAll(r.Body)
		if err == nil {
			body = string(rawBody)
		} else {
			body = ""
		}
		err = responseTemplate.Execute(&buffer, &templateData{
			ServerName: serverName,
			Request:    r,
			Body:       body,
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, "error: %v", err)
		} else {
			_, _ = buffer.WriteTo(w)
		}
	}
}
