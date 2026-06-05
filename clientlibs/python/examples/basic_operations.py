#!/usr/bin/env python3
"""
basic_operations.py - Demonstrates fundamental vector database operations

This example shows how to:
- Create a collection
- Upsert vectors with metadata
- Search for similar vectors
- Retrieve specific vectors by ID
- Delete vectors

Run: python basic_operations.py
"""

import time
from vectron_client import VectronClient, Point


def main():
    # Configuration
    API_GATEWAY_ADDR = "localhost:10010"
    API_KEY = "your-api-key"  # Replace with your actual API key
    COLLECTION_NAME = "basic-demo-collection"
    
    print("🚀 Vectron Basic Operations Demo (Python)")
    print("=" * 50)
    
    # Initialize client
    print("\n1️⃣  Initializing Vectron client...")
    client = VectronClient(
        host=API_GATEWAY_ADDR,
        api_key=API_KEY
    )
    print("✅ Client initialized successfully")
    
    # Create collection
    print("\n2️⃣  Creating collection...")
    dimension = 4  # Small dimension for demo (use 384, 768, or 1536 for real embeddings)
    distance = "euclidean"
    
    try:
        client.create_collection(COLLECTION_NAME, dimension, distance)
        print(f"✅ Collection '{COLLECTION_NAME}' created (dimension={dimension}, distance={distance})")
    except Exception as e:
        print(f"⚠️  Collection may already exist: {e}")
    
    # Wait for collection to be ready
    print("⏳ Waiting for collection to be ready...")
    time.sleep(2)
    
    # Upsert vectors
    print("\n3️⃣  Upserting vectors...")
    points = [
        Point(
            id="product-001",
            vector=[0.1, 0.2, 0.3, 0.4],
            payload={
                "name": "Wireless Headphones",
                "category": "electronics",
                "price": "99.99"
            }
        ),
        Point(
            id="product-002",
            vector=[0.15, 0.25, 0.35, 0.45],
            payload={
                "name": "Bluetooth Speaker",
                "category": "electronics",
                "price": "79.99"
            }
        ),
        Point(
            id="product-003",
            vector=[0.8, 0.7, 0.6, 0.5],
            payload={
                "name": "Running Shoes",
                "category": "sports",
                "price": "129.99"
            }
        ),
        Point(
            id="product-004",
            vector=[0.82, 0.72, 0.62, 0.52],
            payload={
                "name": "Yoga Mat",
                "category": "sports",
                "price": "29.99"
            }
        )
    ]
    
    upserted = client.upsert(COLLECTION_NAME, points)
    print(f"✅ Upserted {upserted} vectors")
    
    # List collections
    print("\n4️⃣  Listing collections...")
    collections = client.list_collections()
    print(f"📚 Collections: {collections}")
    
    # Search for similar vectors
    print("\n5️⃣  Searching for similar vectors...")
    query_vector = [0.12, 0.22, 0.32, 0.42]  # Similar to electronics products
    top_k = 3
    
    results = client.search(COLLECTION_NAME, query_vector, top_k=top_k)
    
    print(f"🔍 Search results (top {top_k}):")
    for i, result in enumerate(results, 1):
        print(f"  {i}. ID: {result.id}, Score: {result.score:.4f}")
        if "name" in result.payload:
            print(f"     Name: {result.payload['name']}")
        if "category" in result.payload:
            print(f"     Category: {result.payload['category']}")
    
    # Get specific vector by ID
    print("\n6️⃣  Retrieving specific vector...")
    point_id = "product-001"
    point = client.get(COLLECTION_NAME, point_id)
    print(f"📄 Point '{point_id}':")
    print(f"   Vector: {point.vector}")
    print(f"   Payload: {point.payload}")
    
    # Another search - sports products
    print("\n7️⃣  Searching for sports products...")
    sports_query = [0.81, 0.71, 0.61, 0.51]  # Similar to sports products
    sports_results = client.search(COLLECTION_NAME, sports_query, top_k=2)
    
    print("🔍 Sports search results:")
    for i, result in enumerate(sports_results, 1):
        print(f"  {i}. ID: {result.id}, Score: {result.score:.4f}, Name: {result.payload.get('name', 'N/A')}")
    
    # Delete a vector
    print("\n8️⃣  Deleting a vector...")
    delete_id = "product-004"
    client.delete(COLLECTION_NAME, delete_id)
    print(f"✅ Deleted point '{delete_id}'")
    
    # Verify deletion
    print("\n9️⃣  Verifying deletion...")
    try:
        client.get(COLLECTION_NAME, delete_id)
        print(f"⚠️  Point '{delete_id}' still exists")
    except Exception:
        print(f"✅ Point '{delete_id}' successfully deleted (not found as expected)")
    
    # Get collection status
    print("\n🔟  Getting collection status...")
    status = client.get_collection_status(COLLECTION_NAME)
    print("📊 Collection Status:")
    print(f"   Name: {status.name}")
    print(f"   Dimension: {status.dimension}")
    print(f"   Distance: {status.distance}")
    print(f"   Shards: {len(status.shards)}")
    for shard in status.shards:
        print(f"     Shard {shard.shard_id}: Ready={shard.ready}, Replicas={len(shard.replicas)}")
    
    # Close client
    client.close()
    
    print("\n✨ Demo completed successfully!")
    print("\nNext steps:")
    print("  - Try ecommerce_search.py for a real-world use case")
    print("  - Try semantic_search.py for document search")
    print("  - Explore the web console at http://localhost:10011")


if __name__ == "__main__":
    main()
