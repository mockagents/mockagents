r/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_ContentTypeJSON()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:269 +0x3e4
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c0001f0c0c by goroutine 327:
  net.isZeros()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:204 +0x28e
  net.IP.To4()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:218 +0x86
  net.IP.appendTo()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:339 +0x6e
  net.IP.String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:314 +0x109
  net.ipEmptyString()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:332 +0x73
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x49
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_ContentTypeJSON()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:269 +0x3e4
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0001f0c0c by goroutine 328:
  syscall.anyToSockaddr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:681 +0x606
  syscall.Getsockname()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:715 +0xb6
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:176 +0x586
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 327 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 328 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_ContentTypeJSON()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:269 +0x3e4
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00034e770 by goroutine 327:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:49 +0x9c
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_ContentTypeJSON()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:269 +0x3e4
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00034e770 by goroutine 328:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x11b
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 327 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 328 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_ContentTypeJSON()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:269 +0x3e4
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00034e768 by goroutine 327:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:52 +0x1f7
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_ContentTypeJSON()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:269 +0x3e4
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00034e768 by goroutine 328:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x104
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 327 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 328 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_ContentTypeJSON()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:269 +0x3e4
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
--- FAIL: TestServer_ContentTypeJSON (0.02s)
    testing.go:1712: race detected during execution of test
==================
WARNING: DATA RACE
Read at 0x00c00023e808 by goroutine 336:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:255 +0x3b
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00023e808 by goroutine 337:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:238 +0x231
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 336 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 337 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022b940 by goroutine 336:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x28
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022b940 by goroutine 337:
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:193 +0x166
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 336 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 337 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c0000f7660 by goroutine 336:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x44
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0000f7660 by goroutine 337:
  net.newFD()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/fd_unix.go:27 +0x104
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:27 +0xc9
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 336 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 337 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00030ac00 by goroutine 336:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x4d
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00030ac00 by goroutine 337:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0xa4
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 336 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 337 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c0003ea24c by goroutine 336:
  net.isZeros()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:204 +0x28e
  net.IP.To4()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:218 +0x86
  net.IP.appendTo()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:339 +0x6e
  net.IP.String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:314 +0x109
  net.ipEmptyString()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:332 +0x73
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x49
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0003ea24c by goroutine 337:
  syscall.anyToSockaddr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:681 +0x606
  syscall.Getsockname()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:715 +0xb6
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:176 +0x586
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 336 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 337 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00030ac20 by goroutine 336:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:49 +0x9c
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00030ac20 by goroutine 337:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x11b
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 336 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 337 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00030ac18 by goroutine 336:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:52 +0x1f7
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:51 +0x865
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00030ac18 by goroutine 337:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x104
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/server.setupTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x33

Goroutine 336 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:130 +0x164

Goroutine 337 (running) created at:
  github.com/mockagents/mockagents/internal/server.setupTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:45 +0x832
  github.com/mockagents/mockagents/internal/server.TestServer_GracefulShutdown()
      /home/runner/work/mock-agents/mock-agents/internal/server/server_test.go:279 +0x36c
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
--- FAIL: TestServer_GracefulShutdown (0.01s)
    testing.go:1712: race detected during execution of test
FAIL
FAIL	github.com/mockagents/mockagents/internal/server	3.456s
ok  	github.com/mockagents/mockagents/internal/storage	2.593s
==================
WARNING: DATA RACE
Read at 0x00c0001fee48 by goroutine 41:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:255 +0x3b
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0001fee48 by goroutine 42:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:238 +0x231
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 41 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 42 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c000183d00 by goroutine 41:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x28
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c000183d00 by goroutine 42:
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:193 +0x166
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 41 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 42 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c000186ee0 by goroutine 41:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x44
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c000186ee0 by goroutine 42:
  net.newFD()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/fd_unix.go:27 +0x104
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:27 +0xc9
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 41 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 42 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022a870 by goroutine 41:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x4d
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022a870 by goroutine 42:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0xa4
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 41 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 42 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c0001c690c by goroutine 41:
  net.isZeros()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:204 +0x28e
  net.IP.To4()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:218 +0x86
  net.IP.appendTo()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:339 +0x6e
  net.IP.String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:314 +0x109
  net.ipEmptyString()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:332 +0x73
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x49
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0001c690c by goroutine 42:
  syscall.anyToSockaddr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:681 +0x606
  syscall.Getsockname()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:715 +0xb6
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:176 +0x586
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 41 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 42 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022a890 by goroutine 41:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:49 +0x9c
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022a890 by goroutine 42:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x11b
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 41 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 42 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022a888 by goroutine 41:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:52 +0x1f7
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022a888 by goroutine 42:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x104
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 41 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 42 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_OpenAIStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:104 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
--- FAIL: TestServerIntegration_OpenAIStreaming (0.02s)
    testing.go:1712: race detected during execution of test
==================
WARNING: DATA RACE
Read at 0x00c0000b09e8 by goroutine 50:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:255 +0x3b
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0000b09e8 by goroutine 51:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:238 +0x231
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 50 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 51 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c0002541c0 by goroutine 50:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x28
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0002541c0 by goroutine 51:
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:193 +0x166
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 50 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 51 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c0001872e0 by goroutine 50:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x44
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0001872e0 by goroutine 51:
  net.newFD()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/fd_unix.go:27 +0x104
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:27 +0xc9
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 50 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 51 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022b650 by goroutine 50:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x4d
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022b650 by goroutine 51:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0xa4
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 50 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 51 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c0001c6acc by goroutine 50:
  net.isZeros()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:204 +0x28e
  net.IP.To4()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:218 +0x86
  net.IP.appendTo()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:339 +0x6e
  net.IP.String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:314 +0x109
  net.ipEmptyString()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:332 +0x73
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x49
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0001c6acc by goroutine 51:
  syscall.anyToSockaddr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:681 +0x606
  syscall.Getsockname()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:715 +0xb6
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:176 +0x586
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 50 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 51 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022b670 by goroutine 50:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:49 +0x9c
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022b670 by goroutine 51:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x11b
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 50 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 51 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022b668 by goroutine 50:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:52 +0x1f7
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022b668 by goroutine 51:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x104
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 50 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 51 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_AnthropicStreaming()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:160 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
--- FAIL: TestServerIntegration_AnthropicStreaming (0.02s)
    testing.go:1712: race detected during execution of test
==================
WARNING: DATA RACE
Read at 0x00c0000b0bc8 by goroutine 60:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:255 +0x3b
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0000b0bc8 by goroutine 61:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:238 +0x231
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 60 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 61 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c000254240 by goroutine 60:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x28
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c000254240 by goroutine 61:
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:193 +0x166
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 60 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 61 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c000187360 by goroutine 60:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x44
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c000187360 by goroutine 61:
  net.newFD()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/fd_unix.go:27 +0x104
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:27 +0xc9
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 60 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 61 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022b7a0 by goroutine 60:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x4d
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022b7a0 by goroutine 61:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0xa4
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 60 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 61 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c0001c6b4c by goroutine 60:
  net.isZeros()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:204 +0x28e
  net.IP.To4()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:218 +0x86
  net.IP.appendTo()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:339 +0x6e
  net.IP.String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:314 +0x109
  net.ipEmptyString()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:332 +0x73
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x49
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0001c6b4c by goroutine 61:
  syscall.anyToSockaddr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:681 +0x606
  syscall.Getsockname()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:715 +0xb6
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:176 +0x586
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 60 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 61 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022b7c0 by goroutine 60:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:49 +0x9c
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022b7c0 by goroutine 61:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x11b
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 60 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 61 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00022b7b8 by goroutine 60:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:52 +0x1f7
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00022b7b8 by goroutine 61:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x104
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 60 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 61 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_NonStreamingStillWorks()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:192 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
--- FAIL: TestServerIntegration_NonStreamingStillWorks (0.02s)
    testing.go:1712: race detected during execution of test
==================
WARNING: DATA RACE
Read at 0x00c0000b12a8 by goroutine 69:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:255 +0x3b
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0000b12a8 by goroutine 70:
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:238 +0x231
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 69 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 70 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00003b180 by goroutine 69:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x28
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00003b180 by goroutine 70:
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:193 +0x166
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 69 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 70 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c0000f6b60 by goroutine 69:
  net.(*TCPListener).Addr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:409 +0x44
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x6d
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c0000f6b60 by goroutine 70:
  net.newFD()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/fd_unix.go:27 +0x104
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:27 +0xc9
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 69 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 70 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c000262300 by goroutine 69:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x4d
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c000262300 by goroutine 70:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0xa4
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 69 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 70 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c00003f70c by goroutine 69:
  net.isZeros()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:204 +0x28e
  net.IP.To4()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:218 +0x86
  net.IP.appendTo()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:339 +0x6e
  net.IP.String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:314 +0x109
  net.ipEmptyString()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ip.go:332 +0x73
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:48 +0x49
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c00003f70c by goroutine 70:
  syscall.anyToSockaddr()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:681 +0x606
  syscall.Getsockname()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/syscall/syscall_linux.go:715 +0xb6
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:176 +0x586
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 69 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 70 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c000262320 by goroutine 69:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:49 +0x9c
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c000262320 by goroutine 70:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x11b
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 69 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 70 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
==================
WARNING: DATA RACE
Read at 0x00c000262318 by goroutine 69:
  net.(*TCPAddr).String()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock.go:52 +0x1f7
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAddr()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:258 +0x76
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:43 +0x785
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38

Previous write at 0x00c000262318 by goroutine 70:
  net.sockaddrToTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:21 +0x104
  net.(*netFD).listenStream()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:177 +0x671
  net.socket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/sock_posix.go:57 +0x2eb
  net.internetSocket()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/ipsock_posix.go:167 +0x126
  net.(*sysListener).listenTCPProto()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/tcpsock_posix.go:189 +0x13e
  net.(*sysListener).listenMPTCP()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/mptcpsock_linux.go:78 +0x77
  net.(*ListenConfig).Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:889 +0x4d1
  net.Listen()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/net/dial.go:968 +0x91
  github.com/mockagents/mockagents/internal/server.(*Server).ListenAndServe()
      /home/runner/work/mock-agents/mock-agents/internal/server/server.go:234 +0x84
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer.func1()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x2e

Goroutine 69 (running) created at:
  testing.(*T).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0xb12
  testing.runTests.func1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2585 +0x85
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.runTests()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2583 +0x9e9
  testing.(*M).Run()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2443 +0xf4b
  main.main()
      _testmain.go:114 +0x164

Goroutine 70 (running) created at:
  github.com/mockagents/mockagents/internal/streaming_test.startTestServer()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:38 +0x750
  github.com/mockagents/mockagents/internal/streaming_test.TestServerIntegration_StreamingWithToolCalls()
      /home/runner/work/mock-agents/mock-agents/internal/streaming/server_integration_test.go:214 +0x92
  testing.tRunner()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2036 +0x21c
  testing.(*T).Run.gowrap1()
      /home/runner/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.26.1.linux-amd64/src/testing/testing.go:2101 +0x38
==================
--- FAIL: TestServerIntegration_StreamingWithToolCalls (0.02s)
    testing.go:1712: race detected during execution of test
FAIL
FAIL	github.com/mockagents/mockagents/internal/streaming	0.275s
ok  	github.com/mockagents/mockagents/internal/tenancy	15.723s
?   	github.com/mockagents/mockagents/internal/types	[no test files]
ok  	github.com/mockagents/mockagents/sdk/go/mockagents	1.084s
FAIL
Error: Process completed with exit code 1.