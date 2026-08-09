# optimux

optimze images on the fly.

## api

Base URL: `https://hostname/resize?image_url=some_url`

Params:
- `format=`, webp and jpe?g
- `sizes=`, width x height
- `encoder=`, empty, progress, json. progress sends only chunks of data at a time, aiming to leverage http2
- `quality=` , values between 1 and 90


## running

only on linux.

```shell
$ make tmpfs.setup
$ make gen.certs
$ make run.dynsc
```

To serve via nginx:

```shell
sudo cp ./nginx.optimux.conf /etc/nginx/sites-enabled
sudo mkdir -p /tmp/shm/edge_cache && sudo chown -R www-data:www-data /tmp/shm/edge_cache
sudo nginx -t
sudo systemctl reload nginx
```

If you want to add a new ENV variable.
- Add it to `Dockerfile`
- Add it to `./infra/supervisord.conf`
- Add it to `./infra/terraform/variables.tf`
- Pass it to the ec2 instance, by changing in `aws_ecs_task_definition` block in `main.tf`
- and `./infra/terraform/ecs-task-definition.json.tmpl`, under `environments`
