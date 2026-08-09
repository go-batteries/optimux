#!/usr/bin/env bash

table="$1"
outdir="$2"

outfile="${table}.schema.sql"

docker run --rm -e PGPASSWORD=welcome1 postgres:15  pg_dump -U postgres -p 5433 -d optimux_stg -h host.docker.internal -t "$table" --schema-only > "${outfile}"

cat "${table}.schema.sql" | awk '/^CREATE TABLE/,/);/' | sed 's/CREATE TABLE public\./CREATE TABLE /' > "${outdir}/${outfile}"

