"""
This module implements the Python client for the Vectron vector database.
It provides a user-friendly, robust interface for all API operations,
including connection handling, authentication, and error handling.
"""
from dataclasses import dataclass, field
from typing import Dict, List

import grpc

from .exceptions import (
    AlreadyExistsError,
    AuthenticationError,
    InternalServerError,
    InvalidArgumentError,
    NotFoundError,
    VectronError,
)

# Assuming the generated protobuf code is in a 'proto' submodule
from .proto import apigateway_pb2, apigateway_pb2_grpc


@dataclass
class Point:
    """Represents a single vector point for upsert operations."""
    id: str
    vector: List[float]
    payload: Dict[str, str] = field(default_factory=dict)


@dataclass
class SearchResult:
    """Represents a single search result, including its ID, score, and payload."""
    id: str
    score: float
    payload: Dict[str, str] = field(default_factory=dict)


class VectronClient:
    """
    The main client for interacting with the Vectron vector database.

    This client handles the gRPC connection and provides methods for all
    of the Vectron API's operations. It can be used as a context manager.
    """

    def __init__(self, host: str, api_key: str = None):
        """
        Initializes the Vectron client.

        Args:
            host: The address of the apigateway gRPC endpoint (e.g., 'localhost:8081').
            api_key: The API key for authentication.
        """
        if not host:
            raise InvalidArgumentError("Host cannot be empty.")
        self.host = host
        self.api_key = api_key
        # For production, use grpc.secure_channel with credentials.
        self._channel = grpc.insecure_channel(host)
        self._stub = apigateway_pb2_grpc.VectronServiceStub(self._channel)

    def _get_metadata(self) -> List[tuple]:
        """Creates the metadata tuple for gRPC authentication."""
        if not self.api_key:
            return []
        return [('authorization', f'Bearer {self.api_key}')]

    def _handle_rpc_error(self, e: grpc.RpcError):
        """Translates gRPC status codes into specific VectronError exceptions."""
        if e.code() == grpc.StatusCode.UNAUTHENTICATED:
            raise AuthenticationError("Authentication failed. Please check your API key.") from e
        elif e.code() == grpc.StatusCode.NOT_FOUND:
            raise NotFoundError(e.details()) from e
        elif e.code() == grpc.StatusCode.INVALID_ARGUMENT:
            raise InvalidArgumentError(e.details()) from e
        elif e.code() == grpc.StatusCode.ALREADY_EXISTS:
            raise AlreadyExistsError(e.details()) from e
        elif e.code() == grpc.StatusCode.INTERNAL:
            raise InternalServerError(e.details()) from e
        else:
            raise VectronError(f"An unexpected gRPC error occurred: {e.details()}") from e

    def create_collection(self, name: str, dimension: int, distance: str = 'euclidean'):
        """
        Creates a new collection in Vectron.

        Args:
            name: The name of the collection. Must be a non-empty string.
            dimension: The dimension of the vectors that will be stored in this collection.
            distance: The distance metric to use ('euclidean', 'cosine', or 'dot').

        Raises:
            InvalidArgumentError: If parameters are invalid.
            AlreadyExistsError: If a collection with the same name already exists.
            VectronError: For other server-side or connection errors.
        """
        if not name:
            raise InvalidArgumentError("Collection name cannot be empty.")
        if dimension <= 0:
            raise InvalidArgumentError("Dimension must be a positive integer.")

        request = apigateway_pb2.CreateCollectionRequest(
            name=name,
            dimension=dimension,
            distance=distance,
        )
        try:
            self._stub.CreateCollection(request, metadata=self._get_metadata())
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def list_collections(self) -> List[str]:
        """
        Retrieves a list of all collection names.

        Returns:
            A list of strings, where each string is a collection name.

        Raises:
            VectronError: For server-side or connection errors.
        """
        request = apigateway_pb2.ListCollectionsRequest()
        try:
            response = self._stub.ListCollections(request, metadata=self._get_metadata())
            return list(response.collections)
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def upsert(self, collection: str, points: List[Point]) -> int:
        """
        Inserts or updates vectors in a specified collection.

        Args:
            collection: The name of the collection to upsert into.
            points: A list of `Point` objects.

        Returns:
            The number of points that were successfully upserted.

        Raises:
            InvalidArgumentError: If parameters are invalid (e.g., empty collection name or point ID).
            NotFoundError: If the specified collection does not exist.
            VectronError: For other server-side or connection errors.
        """
        if not collection:
            raise InvalidArgumentError("Collection name cannot be empty.")
        if not points:
            return 0

        proto_points = []
        for p in points:
            if not p.id:
                raise InvalidArgumentError("Point ID cannot be empty.")
            proto_points.append(apigateway_pb2.Point(id=p.id, vector=p.vector, payload=p.payload))

        request = apigateway_pb2.UpsertRequest(
            collection=collection,
            points=proto_points,
        )
        try:
            response = self._stub.Upsert(request, metadata=self._get_metadata())
            return response.upserted
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def search(self, collection: str, vector: List[float], top_k: int = 10) -> List[SearchResult]:
        """
        Searches a collection for the k vectors most similar to the query vector.

        Args:
            collection: The name of the collection to search in.
            vector: The query vector.
            top_k: The number of nearest neighbors to return.

        Returns:
            A list of `SearchResult` objects, ordered by similarity.

        Raises:
            InvalidArgumentError: If parameters are invalid.
            NotFoundError: If the specified collection does not exist.
            VectronError: For other server-side or connection errors.
        """
        if not collection:
            raise InvalidArgumentError("Collection name cannot be empty.")

        request = apigateway_pb2.SearchRequest(
            collection=collection,
            vector=vector,
            top_k=top_k,
        )
        try:
            response = self._stub.Search(request, metadata=self._get_metadata())
            results = [
                SearchResult(id=r.id, score=r.score, payload=r.payload)
                for r in response.results
            ]
            return results
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def get(self, collection: str, point_id: str) -> Point:
        """
        Retrieves a single point by its ID from a collection.

        Args:
            collection: The name of the collection.
            point_id: The ID of the point to retrieve.

        Returns:
            The requested `Point` object.

        Raises:
            InvalidArgumentError: If parameters are invalid.
            NotFoundError: If the collection or point does not exist.
            VectronError: For other server-side or connection errors.
        """
        if not collection or not point_id:
            raise InvalidArgumentError("Collection name and point ID cannot be empty.")

        request = apigateway_pb2.GetRequest(collection=collection, id=point_id)
        try:
            response = self._stub.Get(request, metadata=self._get_metadata())
            p = response.point
            return Point(id=p.id, vector=p.vector, payload=p.payload)
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def delete(self, collection: str, point_id: str):
        """
        Deletes a single point by its ID from a collection.

        Args:
            collection: The name of the collection.
            point_id: The ID of the point to delete.

        Raises:
            InvalidArgumentError: If parameters are invalid.
            NotFoundError: If the collection or point does not exist.
            VectronError: For other server-side or connection errors.
        """
        if not collection or not point_id:
            raise InvalidArgumentError("Collection name and point ID cannot be empty.")

        request = apigateway_pb2.DeleteRequest(collection=collection, id=point_id)
        try:
            self._stub.Delete(request, metadata=self._get_metadata())
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def close(self):
        """Closes the underlying gRPC channel."""
        if self._channel:
            self._channel.close()

    def __enter__(self):
        """Allows the client to be used as a context manager."""
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Closes the channel when exiting the context."""
        self.close()
