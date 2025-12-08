from dataclasses import dataclass, field
from typing import Any, Dict, List

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
    """Represents a single vector point."""
    id: str
    vector: List[float]
    payload: Dict[str, str] = field(default_factory=dict)

@dataclass
class SearchResult:
    """Represents a single search result."""
    id: str
    score: float
    payload: Dict[str, str] = field(default_factory=dict)

class VectronClient:
    """
    The Python client for the Vectron vector database.

    This client provides a convenient and robust interface for interacting
    with the Vectron `apigateway`.
    """

    def __init__(self, host: str, api_key: str = None):
        """
        Initializes the Vectron client.

        Args:
            host: The address of the apigateway (e.g., 'localhost:8080').
            api_key: The API key for authentication.
        """
        if not host:
            raise InvalidArgumentError("Host cannot be empty.")
        self.host = host
        self.api_key = api_key
        self._channel = grpc.insecure_channel(host)
        self._stub = apigateway_pb2_grpc.VectronServiceStub(self._channel)

    def _get_metadata(self):
        if not self.api_key:
            return []
        return [('authorization', f'Bearer {self.api_key}')]

    def _handle_rpc_error(self, e: grpc.RpcError):
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
            raise VectronError(f"An unexpected error occurred: {e.details()}") from e

    def create_collection(self, name: str, dimension: int, distance: str = 'euclidean'):
        """
        Creates a new collection.

        Args:
            name: The name of the collection. Must be a non-empty string.
            dimension: The dimension of the vectors in the collection. Must be a positive integer.
            distance: The distance metric to use ('euclidean', 'cosine', or 'dot'). Defaults to 'euclidean'.

        Raises:
            InvalidArgumentError: If parameters are invalid.
            AlreadyExistsError: If the collection already exists.
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
        Lists all collections.

        Returns:
            A list of collection names.

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
        Inserts or updates vectors in a collection.

        Args:
            collection: The name of the collection.
            points: A list of `Point` objects to upsert.

        Returns:
            The number of points upserted.

        Raises:
            InvalidArgumentError: If parameters are invalid.
            NotFoundError: If the collection does not exist.
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
        Searches for the k nearest neighbors to a query vector.

        Args:
            collection: The name of the collection.
            vector: The query vector.
            top_k: The number of results to return.

        Returns:
            A list of `SearchResult` objects.

        Raises:
            InvalidArgumentError: If parameters are invalid.
            NotFoundError: If the collection does not exist.
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
            results = []
            for r in response.results:
                results.append(SearchResult(id=r.id, score=r.score, payload=r.payload))
            return results
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def get(self, collection: str, point_id: str) -> Point:
        """
        Retrieves a point by its ID.

        Args:
            collection: The name of the collection.
            point_id: The ID of the point to retrieve.

        Returns:
            The `Point` object.

        Raises:
            InvalidArgumentError: If parameters are invalid.
            NotFoundError: If the collection or point does not exist.
            VectronError: For other server-side or connection errors.
        """
        if not collection or not point_id:
            raise InvalidArgumentError("Collection name and point ID cannot be empty.")

        request = apigateway_pb2.GetRequest(
            collection=collection,
            id=point_id,
        )
        try:
            response = self._stub.Get(request, metadata=self._get_metadata())
            p = response.point
            return Point(id=p.id, vector=p.vector, payload=p.payload)
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def delete(self, collection: str, point_id: str):
        """
        Deletes a point by its ID.

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

        request = apigateway_pb2.DeleteRequest(
            collection=collection,
            id=point_id,
        )
        try:
            self._stub.Delete(request, metadata=self._get_metadata())
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def close(self):
        """Closes the gRPC channel."""
        self._channel.close()

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.close()
