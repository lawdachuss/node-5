#!/bin/bash

# Start FlareSolverr in Docker
# This script runs FlareSolverr as a standalone container

echo "Starting FlareSolverr on port 8191..."

docker run -d \
  --name flaresolverr \
  --restart unless-stopped \
  -p 8191:8191 \
  -e LOG_LEVEL=info \
  -e LOG_HTML=false \
  -e CAPTCHA_SOLVER=none \
  -e TZ=UTC \
  ghcr.io/flaresolverr/flaresolverr:latest

echo "FlareSolverr started!"
echo "Access it at: http://127.0.0.1:8191/v1"
echo ""
echo "To check status: docker logs flaresolverr"
echo "To stop: docker stop flaresolverr"
echo "To remove: docker rm flaresolverr"
