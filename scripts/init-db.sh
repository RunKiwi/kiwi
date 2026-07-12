#!/bin/bash
set -e

# This script is executed by the postgres container on initialization
# It uses the migrations/0001_initial_v2_schema.up.sql file

echo "Running initial V2 schema migrations..."

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" -f /docker-entrypoint-initdb.d/migrations/0001_initial_v2_schema.up.sql

echo "Migrations completed successfully."
