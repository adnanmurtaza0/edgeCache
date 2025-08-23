#!/usr/bin/env bash
set -euo pipefail

echo "Warm cache (first MISS expected):"
curl -s -i "http://localhost:3000/asset?path=/hello.txt" | grep -E 'HTTP/|X-Selected-Edge|X-Cache|Content-Type' || true
echo

echo "Second request (HIT expected):"
curl -s -i "http://localhost:3000/asset?path=/hello.txt" | grep -E 'HTTP/|X-Selected-Edge|X-Cache|Content-Type' || true
echo

echo "Invalidate path across all edges:"
curl -s -X POST localhost:8081/invalidate -H "Content-Type: application/json" -d '{"path":"/hello.txt"}'
echo

sleep 1
echo "After invalidation (MISS expected again):"
curl -s -i "http://localhost:3000/asset?path=/hello.txt" | grep -E 'HTTP/|X-Selected-Edge|X-Cache|Content-Type' || true
echo
