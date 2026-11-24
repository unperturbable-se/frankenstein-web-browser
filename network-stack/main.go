package main

import (
	"net"
	"net/url"
	"strings"
)



func parseURL(s string) (scheme, host, path, query string) {
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return
	}
	return u.Scheme, u.Host, u.Path, u.RawQuery
}



func dnsResolver(s string) string {
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	addrs, err := net.LookupHost(s)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	return addrs[0]
}



func createTCPConnection(ip, port string) (bool, net.Conn) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(ip, port), 5*time.Second)
	if err != nil {
		return false, nil // connection failed
	}
	return true, conn // connection success
}


func startTLShandshake(connection net.Conn ){
	// return encrypted bool , session_key string
}

func httpGET(connection,session_key string,encrypted bool){
	// return httpRequest string
}

func httpPOST(connection,session_key string,encrypted bool){
	// return httpRequest string
}

func sendRequest(httpRequest string){
	// send request using http/1
	// return webData string
}

func createFiles(webData string){
	// breaks down the webdata into the different file chunks
	// then creates those files in the cwd
}


	
func main(){
	fmt.Println("hello world")
}