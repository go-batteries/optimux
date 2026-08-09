# deployments

### docker-compose

A docker-compose file has been provided, which uses dev.nginx.conf to create 3 instances of the app,
and one instance of nginx.
The `upstream` config in nginx can be computed and templatized.

### nomad, consul and ansible

- Use ansible or terraform to setup the servers.
- This involves setting up `docker`, `nomad`, and `consul`.
- for local testing, install them on your machine, if you are not on debian/ubuntu.
- use `make build.docker.local` to build the docker image to be used.
- we can't tag the `docker image` as `some-server:latest`, the `latest` makes it force pull from docker remote.
- **consul** is used to manage the auto registration and de-registration of app boxes to nginx `upstream` block. 
- **nomad** deployment itself can work without consul.

#### build and run

```shell
$> make build.docker.local # to build the docker image
$> consul agent -server -bootstrap-expect=1 -bind=127.0.0.1 -client=0.0.0.0 -data-dir=./tmp/ # start the consul server
$> nomad agent -dev # to start the nomad agent
$> nomad job plan ./infra/deploys/app.nomad.hcl # check if plan is success, run into issues like cpu/memory/bandwith out of capacity

$> nomad job run ./infra/deploys/app.nomad.hcl # if plan success execute deployment
$> nomad job status app  # check job status. `app` is the `job` name as mentioned in the hcl file
```
