package main

import (
	"net"
	"net/url"
	"strings"
	"time"
	"crypto/tls"
	"fmt"
	"io"
	"bufio"
	"net/http"
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

func startTLSHandshake(conn net.Conn, host string) (bool, *tls.Conn) {
	tlsConn := tls.Client(conn, &tls.Config{ServerName: host})
	if err := tlsConn.Handshake(); err != nil {
		return false, nil
	}
	return true, tlsConn
}

func httpGET(address, parameter string) string {
	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	u, err := url.Parse(address)
	if err != nil {
		return ""
	}
	if parameter != "" {
		if u.RawQuery != "" {
			u.RawQuery += "&"
		}
		u.RawQuery += parameter
	}
	return fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUser-Agent: Mozilla/5.0 (compatible; go-browser/1.0)\r\nAccept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\nAccept-Language: en-US,en;q=0.9\r\nAccept-Encoding: identity\r\nConnection: keep-alive\r\n\r\n", u.RequestURI(), u.Host)
}


func httpPOST(address, message string) string {
	_, host, path, _ := parseURL(address)
	if path == "" {
		path = "/"
	}
	return fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", path, host, len(message), message)
}

// iteratively reads chunks until the whole body is complete :)
func sendRequest(httpRequest string, conn net.Conn) (webData string, complete bool) {
	if conn == nil {
		return "", false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	if _, err := io.Copy(conn, strings.NewReader(httpRequest)); err != nil {
		return "", false
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return "", false // malformed status line / headers
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return string(body), false // died mid-body → partial
	}
	return string(body), true
}

func createFiles(webData string) {
	// breaks down the webdata into the different file chunks
	// then creates those files in the cwd
}

//func http1_1(address string)(already_present bool,contents string,filetype string) {
//	
//	_, host, _, _ := parseURL(address)
//	ip_address := dnsResolver(host)
//	if ip_address in set{return true,nill}
//	fmt.Println("IP address:", ip_address)
//
//	connected, connection := createTCPConnection(ip_address, "443")
//	if !connected {
//		fmt.Println("tcp connection failed")
//		return
//	}
//
//	connected, tls_connection := startTLSHandshake(connection, host)
//	if !connected {
//		fmt.Println("tls handshake failed")
//		connection.Close()
//		return
//	}
//
//	reply, complete := sendRequest(httpGET(address, ""), tls_connection)
//	
//	if !complete{
//		fmt.Println("Request sending failure")
//	}
//	//get all the links present in the reply and add it to a global queue from which links are fetched and then called via http1(link)
//	return false,reply
//}