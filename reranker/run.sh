#!/bin/bash
# Quick test script for the reranker service

set -e

RERANKER_PORT=${RERANKER_PORT:-50051}
RERANKER_STRATEGY=${RERANKER_STRATEGY:-rule}

echo "Starting Reranker Service..."
echo "Port: $RERANKER_PORT"
echo "Strategy: $RERANKER_STRATEGY"
echo ""

# Set example config for rule-based strategy
export RULE_EXACT_MATCH_BOOST=0.3
export RULE_TITLE_BOOST=0.2
export RULE_METADATA_BOOSTS="verified:0.3,featured:0.2"
export RULE_METADATA_PENALTIES="deprecated:0.5"

./bin/reranker \
  --port="$RERANKER_PORT" \
  --strategy="$RERANKER_STRATEGY" \
  --cache=memory
