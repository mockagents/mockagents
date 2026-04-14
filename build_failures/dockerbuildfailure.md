Run docker build -t mockagents:ci-test .
#0 building with "default" instance using docker driver

#1 [internal] load build definition from Dockerfile
#1 transferring dockerfile: 1.05kB done
#1 DONE 0.0s

#2 [internal] load metadata for docker.io/library/golang:1.22-alpine
#2 ...

#3 [auth] library/alpine:pull token for registry-1.docker.io
#3 DONE 0.0s

#4 [auth] library/golang:pull token for registry-1.docker.io
#4 DONE 0.0s

#5 [internal] load metadata for docker.io/library/alpine:3.19
#5 DONE 1.4s

#2 [internal] load metadata for docker.io/library/golang:1.22-alpine
#2 DONE 1.4s

#6 [internal] load .dockerignore
#6 transferring context: 182B done
#6 DONE 0.0s

#7 [builder 1/7] FROM docker.io/library/golang:1.22-alpine@sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052
#7 resolve docker.io/library/golang:1.22-alpine@sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052 0.0s done
#7 sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052 10.30kB / 10.30kB done
#7 sha256:6d405dfc5fdf3a45df1529cf060b920041f52ce523487e0f36f02765af294a51 1.92kB / 1.92kB done
#7 ...

#8 [internal] load build context
#8 transferring context: 979.35kB 0.0s done
#8 DONE 0.3s

#7 [builder 1/7] FROM docker.io/library/golang:1.22-alpine@sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052
#7 sha256:4129f51f28c9ae5de799b958ba2aaa8f92f26cc7bf47c107891673fe4b516c03 2.08kB / 2.08kB done
#7 sha256:1f3e46996e2966e4faa5846e56e76e3748b7315e2ded61476c24403d592134f0 3.64MB / 3.64MB 0.2s
#7 sha256:4d75fd4b73869ed224045c010cdec78756eefb6752a5a8e4804294009eac11e9 294.90kB / 294.90kB 0.2s
#7 sha256:1f3e46996e2966e4faa5846e56e76e3748b7315e2ded61476c24403d592134f0 3.64MB / 3.64MB 0.3s done
#7 sha256:4d75fd4b73869ed224045c010cdec78756eefb6752a5a8e4804294009eac11e9 294.90kB / 294.90kB 0.3s done
#7 extracting sha256:1f3e46996e2966e4faa5846e56e76e3748b7315e2ded61476c24403d592134f0 0.1s done
#7 sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 0B / 69.36MB 0.3s
#7 sha256:5f837c998576dcb54bc285997f33fcc2166dff6aa48fe3a374da92474efd5fe8 0B / 126B 0.3s
#7 sha256:4f4fb700ef54461cfa02571ae0db9a0dc1e0cdb5577484a6d75e68dc38e8acc1 0B / 32B 0.3s
#7 extracting sha256:4d75fd4b73869ed224045c010cdec78756eefb6752a5a8e4804294009eac11e9
#7 ...

#9 [stage-1 1/4] FROM docker.io/library/alpine:3.19@sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1
#9 resolve docker.io/library/alpine:3.19@sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1 0.0s done
#9 sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1 8.08kB / 8.08kB done
#9 sha256:b58899f069c47216f6002a6850143dc6fae0d35eb8b0df9300bbe6327b9c2171 1.02kB / 1.02kB done
#9 sha256:83b2b6703a620bf2e001ab57f7adc414d891787b3c59859b1b62909e48dd2242 581B / 581B done
#9 sha256:17a39c0ba978cc27001e9c56a480f98106e1ab74bd56eb302f9fd4cf758ea43f 3.42MB / 3.42MB 0.3s done
#9 extracting sha256:17a39c0ba978cc27001e9c56a480f98106e1ab74bd56eb302f9fd4cf758ea43f 0.1s done
#9 DONE 0.4s

#7 [builder 1/7] FROM docker.io/library/golang:1.22-alpine@sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052
#7 sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 15.73MB / 69.36MB 0.5s
#7 sha256:5f837c998576dcb54bc285997f33fcc2166dff6aa48fe3a374da92474efd5fe8 126B / 126B 0.5s
#7 sha256:4f4fb700ef54461cfa02571ae0db9a0dc1e0cdb5577484a6d75e68dc38e8acc1 32B / 32B 0.5s
#7 sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 26.21MB / 69.36MB 0.7s
#7 sha256:4f4fb700ef54461cfa02571ae0db9a0dc1e0cdb5577484a6d75e68dc38e8acc1 32B / 32B 0.6s done
#7 sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 34.60MB / 69.36MB 0.9s
#7 sha256:5f837c998576dcb54bc285997f33fcc2166dff6aa48fe3a374da92474efd5fe8 126B / 126B 0.7s done
#7 extracting sha256:4d75fd4b73869ed224045c010cdec78756eefb6752a5a8e4804294009eac11e9 0.6s done
#7 sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 46.14MB / 69.36MB 1.2s
#7 sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 53.48MB / 69.36MB 1.4s
#7 sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 61.87MB / 69.36MB 1.6s
#7 sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 67.11MB / 69.36MB 1.7s
#7 sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 69.36MB / 69.36MB 3.1s done
#7 ...

#10 [stage-1 2/4] RUN apk add --no-cache ca-certificates     && addgroup -S mockagents     && adduser -S mockagents -G mockagents
#10 2.347 fetch https://dl-cdn.alpinelinux.org/alpine/v3.19/main/x86_64/APKINDEX.tar.gz
#10 2.410 fetch https://dl-cdn.alpinelinux.org/alpine/v3.19/community/x86_64/APKINDEX.tar.gz
#10 2.586 (1/1) Installing ca-certificates (20250911-r0)
#10 2.945 Executing busybox-1.36.1-r20.trigger
#10 3.715 Executing ca-certificates-20250911-r0.trigger
#10 4.260 OK: 8 MiB in 16 packages
#10 DONE 12.8s

#7 [builder 1/7] FROM docker.io/library/golang:1.22-alpine@sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052
#7 extracting sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 0.1s
#7 extracting sha256:afa154b433c7f72db064d19e1bcfa84ee196ad29120328f6bdb2c5fbd7b8eeac 2.9s done
#7 extracting sha256:5f837c998576dcb54bc285997f33fcc2166dff6aa48fe3a374da92474efd5fe8
#7 extracting sha256:5f837c998576dcb54bc285997f33fcc2166dff6aa48fe3a374da92474efd5fe8 done
#7 extracting sha256:4f4fb700ef54461cfa02571ae0db9a0dc1e0cdb5577484a6d75e68dc38e8acc1 done
#7 DONE 19.7s

#11 [builder 2/7] RUN apk add --no-cache git ca-certificates
#11 0.107 fetch https://dl-cdn.alpinelinux.org/alpine/v3.21/main/x86_64/APKINDEX.tar.gz
#11 0.164 fetch https://dl-cdn.alpinelinux.org/alpine/v3.21/community/x86_64/APKINDEX.tar.gz
#11 0.382 (1/12) Installing brotli-libs (1.1.0-r2)
#11 0.393 (2/12) Installing c-ares (1.34.6-r0)
#11 0.396 (3/12) Installing libunistring (1.2-r0)
#11 0.405 (4/12) Installing libidn2 (2.3.7-r0)
#11 0.408 (5/12) Installing nghttp2-libs (1.64.0-r0)
#11 0.410 (6/12) Installing libpsl (0.21.5-r3)
#11 0.413 (7/12) Installing zstd-libs (1.5.6-r2)
#11 0.421 (8/12) Installing libcurl (8.14.1-r2)
#11 0.428 (9/12) Installing libexpat (2.7.5-r0)
#11 0.430 (10/12) Installing pcre2 (10.43-r0)
#11 0.436 (11/12) Installing git (2.47.3-r0)
#11 0.507 (12/12) Installing git-init-template (2.47.3-r0)
#11 0.509 Executing busybox-1.37.0-r9.trigger
#11 0.514 OK: 19 MiB in 28 packages
#11 DONE 0.7s

#12 [builder 3/7] WORKDIR /src
#12 DONE 0.0s

#13 [builder 4/7] COPY go.mod go.sum ./
#13 DONE 0.0s

#14 [builder 5/7] RUN go mod download
#14 0.816 go: go.mod requires go >= 1.26.1 (running go 1.22.12; GOTOOLCHAIN=local)
#14 ERROR: process "/bin/sh -c go mod download" did not complete successfully: exit code: 1
------
 > [builder 5/7] RUN go mod download:
0.816 go: go.mod requires go >= 1.26.1 (running go 1.22.12; GOTOOLCHAIN=local)
------
Dockerfile:10
--------------------
   8 |     # Cache dependency downloads.
   9 |     COPY go.mod go.sum ./
  10 | >>> RUN go mod download
  11 |     
  12 |     # Copy source and build.
--------------------
ERROR: failed to build: failed to solve: process "/bin/sh -c go mod download" did not complete successfully: exit code: 1
Error: Process completed with exit code 1.
