"""
This module implements the Python client for the Vectron vector database.
It provides a user-friendly, robust interface for all API operations,
including connection handling, authentication, and error handling.
"""
from dataclasses import dataclass, field
import random
import threading
import time
from typing import Dict, List, Optional, Tuple, Any, Callable

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
from .proto.apigateway import apigateway_pb2, apigateway_pb2_grpc


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


@dataclass
class ClientOptions:
    """Configuration for the Vectron client."""
    timeout_seconds: float = 10.0
    use_tls: bool = False
    tls_root_certificates: Optional[bytes] = None
    tls_server_name: Optional[str] = None
    max_recv_message_length: int = 16 * 1024 * 1024
    max_send_message_length: int = 16 * 1024 * 1024
    keepalive_time_ms: int = 30000
    keepalive_timeout_ms: int = 10000
    keepalive_permit_without_calls: bool = False
    expected_vector_dim: int = 0
    compression: Optional[str] = None
    hedged_reads: bool = False
    hedge_delay_ms: int = 75
    max_batch_bytes: int = 0
    retry_max_attempts: int = 3
    retry_initial_backoff_ms: int = 100
    retry_max_backoff_ms: int = 2000
    retry_backoff_multiplier: float = 2.0
    retry_jitter: float = 0.2
    retry_on_writes: bool = False
    retryable_statuses: Optional[List[grpc.StatusCode]] = None


class VectronClient:
    """
    The main client for interacting with the Vectron vector database.

    This client handles the gRPC connection and provides methods for all
    of the Vectron API's operations. It can be used as a context manager.
    """

    def __init__(self, host: str, api_key: str = None, options: Optional[ClientOptions] = None):
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
        self.options = options or ClientOptions()
        self._metadata = self._get_metadata()

        channel_options = []
        if self.options.max_recv_message_length:
            channel_options.append(("grpc.max_receive_message_length", self.options.max_recv_message_length))
        if self.options.max_send_message_length:
            channel_options.append(("grpc.max_send_message_length", self.options.max_send_message_length))
        if self.options.keepalive_time_ms:
            channel_options.append(("grpc.keepalive_time_ms", self.options.keepalive_time_ms))
        if self.options.keepalive_timeout_ms:
            channel_options.append(("grpc.keepalive_timeout_ms", self.options.keepalive_timeout_ms))
        channel_options.append(
            ("grpc.keepalive_permit_without_calls", int(self.options.keepalive_permit_without_calls))
        )
        if self.options.tls_server_name:
            channel_options.append(("grpc.ssl_target_name_override", self.options.tls_server_name))

        if self.options.use_tls:
            credentials = grpc.ssl_channel_credentials(root_certificates=self.options.tls_root_certificates)
            self._channel = grpc.secure_channel(host, credentials, options=channel_options)
        else:
            # For production, use grpc.secure_channel with credentials.
            self._channel = grpc.insecure_channel(host, options=channel_options)

        self._stub = apigateway_pb2_grpc.VectronServiceStub(self._channel)

    def _get_metadata(self) -> List[tuple]:
        """Creates the metadata tuple for gRPC authentication."""
        if not self.api_key:
            return []
        return [('authorization', f'Bearer {self.api_key}')]

    def _get_compression(self):
        if not self.options.compression:
            return None
        if self.options.compression.lower() == "gzip":
            return grpc.Compression.Gzip
        return None

    def _get_retryable_statuses(self) -> List[grpc.StatusCode]:
        if self.options.retryable_statuses is not None:
            return self.options.retryable_statuses
        return [
            grpc.StatusCode.UNAVAILABLE,
            grpc.StatusCode.DEADLINE_EXCEEDED,
            grpc.StatusCode.RESOURCE_EXHAUSTED,
        ]

    def _should_retry(self, e: grpc.RpcError, is_write: bool, attempt: int) -> bool:
        if self.options.retry_max_attempts <= 1 or attempt >= self.options.retry_max_attempts:
            return False
        if is_write and not self.options.retry_on_writes:
            return False
        code = e.code()
        return code in self._get_retryable_statuses()

    def _compute_backoff_seconds(self, attempt: int) -> float:
        backoff = max(self.options.retry_initial_backoff_ms, 1)
        for _ in range(1, attempt):
            backoff = int(backoff * max(self.options.retry_backoff_multiplier, 2.0))
            if self.options.retry_max_backoff_ms and backoff > self.options.retry_max_backoff_ms:
                backoff = self.options.retry_max_backoff_ms
                break
        if self.options.retry_jitter and self.options.retry_jitter > 0:
            jitter = (2 * random.random() - 1) * self.options.retry_jitter
            backoff = int(backoff * (1 + jitter))
            if backoff < 0:
                backoff = 0
        return backoff / 1000.0

    def _call_with_retry(self, func: Callable[[], Any], is_write: bool):
        attempt = 1
        while True:
            try:
                return func()
            except grpc.RpcError as e:
                if not self._should_retry(e, is_write, attempt):
                    self._handle_rpc_error(e)
                time.sleep(self._compute_backoff_seconds(attempt))
                attempt += 1

    def _hedged_call(self, func: Callable[[], Any]) -> Any:
        if not self.options.hedged_reads:
            return func()

        result = {"value": None, "error": None}
        lock = threading.Lock()
        done = threading.Event()
        errors: List[grpc.RpcError] = []

        def run_call():
            try:
                value = func()
                with lock:
                    if not done.is_set():
                        result["value"] = value
                        done.set()
            except grpc.RpcError as e:
                with lock:
                    errors.append(e)
                    if len(errors) >= 2 and not done.is_set():
                        result["error"] = errors[0]
                        done.set()

        t1 = threading.Thread(target=run_call, daemon=True)
        t1.start()

        delay = max(self.options.hedge_delay_ms, 0) / 1000.0
        if delay > 0:
            if done.wait(delay):
                if result["error"] is not None:
                    raise result["error"]
                return result["value"]

        t2 = threading.Thread(target=run_call, daemon=True)
        t2.start()

        done.wait()
        if result["error"] is not None:
            raise result["error"]
        return result["value"]

    def _validate_vector_dim(self, vector: List[float]):
        if self.options.expected_vector_dim > 0 and len(vector) != self.options.expected_vector_dim:
            raise InvalidArgumentError(
                f"Vector dimension {len(vector)} does not match expected {self.options.expected_vector_dim}."
            )

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
            self._call_with_retry(
                lambda: self._stub.CreateCollection(
                    request,
                    metadata=self._metadata,
                    timeout=self.options.timeout_seconds if self.options.timeout_seconds > 0 else None,
                    compression=self._get_compression(),
                ),
                is_write=True,
            )
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
            response = self._call_with_retry(
                lambda: self._hedged_call(
                    lambda: self._stub.ListCollections(
                        request,
                        metadata=self._metadata,
                        timeout=self.options.timeout_seconds if self.options.timeout_seconds > 0 else None,
                        compression=self._get_compression(),
                    )
                ),
                is_write=False,
            )
            return list(response.collections)
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def get_collection_status(self, name: str):
        """
        Retrieves the status of a collection, including the readiness of its shards.

        Args:
            name: The name of the collection.

        Returns:
            The collection status.

        Raises:
            InvalidArgumentError: If the collection name is empty.
            NotFoundError: If the collection does not exist.
            VectronError: For other server-side or connection errors.
        """
        if not name:
            raise InvalidArgumentError("Collection name cannot be empty.")

        request = apigateway_pb2.GetCollectionStatusRequest(name=name)
        try:
            response = self._call_with_retry(
                lambda: self._hedged_call(
                    lambda: self._stub.GetCollectionStatus(
                        request,
                        metadata=self._metadata,
                        timeout=self.options.timeout_seconds if self.options.timeout_seconds > 0 else None,
                        compression=self._get_compression(),
                    )
                ),
                is_write=False,
            )
            return response
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
            if not p.vector:
                raise InvalidArgumentError("Point vector cannot be empty.")
            self._validate_vector_dim(p.vector)
            proto_points.append(apigateway_pb2.Point(id=p.id, vector=p.vector, payload=p.payload))

        request = apigateway_pb2.UpsertRequest(
            collection=collection,
            points=proto_points,
        )
        try:
            response = self._call_with_retry(
                lambda: self._stub.Upsert(
                    request,
                    metadata=self._metadata,
                    timeout=self.options.timeout_seconds if self.options.timeout_seconds > 0 else None,
                    compression=self._get_compression(),
                ),
                is_write=True,
            )
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
        if not vector:
            raise InvalidArgumentError("Query vector cannot be empty.")
        self._validate_vector_dim(vector)

        request = apigateway_pb2.SearchRequest(
            collection=collection,
            vector=vector,
            top_k=top_k,
        )
        try:
            response = self._call_with_retry(
                lambda: self._hedged_call(
                    lambda: self._stub.Search(
                        request,
                        metadata=self._metadata,
                        timeout=self.options.timeout_seconds if self.options.timeout_seconds > 0 else None,
                        compression=self._get_compression(),
                    )
                ),
                is_write=False,
            )
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
            response = self._call_with_retry(
                lambda: self._hedged_call(
                    lambda: self._stub.Get(
                        request,
                        metadata=self._metadata,
                        timeout=self.options.timeout_seconds if self.options.timeout_seconds > 0 else None,
                        compression=self._get_compression(),
                    )
                ),
                is_write=False,
            )
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
            self._call_with_retry(
                lambda: self._stub.Delete(
                    request,
                    metadata=self._metadata,
                    timeout=self.options.timeout_seconds if self.options.timeout_seconds > 0 else None,
                    compression=self._get_compression(),
                ),
                is_write=True,
            )
        except grpc.RpcError as e:
            self._handle_rpc_error(e)

    def close(self):
        """Closes the underlying gRPC channel."""
        if self._channel:
            self._channel.close()

    def upsert_batch(self, collection: str, points: List[Point], batch_size: int = 256, concurrency: int = 1) -> int:
        """Splits large upserts into batches for higher throughput."""
        if batch_size <= 0:
            batch_size = 256
        if concurrency <= 0:
            concurrency = 1
        if not points:
            return 0

        total = 0
        total_lock = threading.Lock()
        err_lock = threading.Lock()
        first_error: List[Exception] = []

        def worker(chunks: List[List[Point]]):
            nonlocal total
            for batch in chunks:
                if first_error:
                    return
                try:
                    count = self.upsert(collection, batch)
                    with total_lock:
                        total += count
                except Exception as e:
                    with err_lock:
                        if not first_error:
                            first_error.append(e)
                    return

        max_batch_bytes = getattr(self.options, "max_batch_bytes", 0)

        def estimate_point_bytes(point: Point) -> int:
            size = len(point.id) + len(point.vector) * 4
            for k, v in point.payload.items():
                size += len(k) + len(v)
            return size + 16

        batches: List[List[Point]] = []
        if max_batch_bytes and max_batch_bytes > 0:
            idx = 0
            while idx < len(points):
                current: List[Point] = []
                current_bytes = 0
                while idx < len(points) and len(current) < batch_size:
                    p = points[idx]
                    p_bytes = estimate_point_bytes(p)
                    if not current or current_bytes + p_bytes <= max_batch_bytes:
                        current.append(p)
                        current_bytes += p_bytes
                        idx += 1
                        continue
                    break
                batches.append(current)
        else:
            batches = [points[i:i + batch_size] for i in range(0, len(points), batch_size)]
        if concurrency == 1:
            worker(batches)
        else:
            threads = []
            for i in range(concurrency):
                chunk = batches[i::concurrency]
                t = threading.Thread(target=worker, args=(chunk,), daemon=True)
                threads.append(t)
                t.start()
            for t in threads:
                t.join()

        if first_error:
            raise first_error[0]
        return total

    def __enter__(self):
        """Allows the client to be used as a context manager."""
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        """Closes the channel when exiting the context."""
        self.close()

    @staticmethod
    def help() -> Tuple[str, Dict[str, Any]]:
        """Returns a human-readable help message and a structured options map."""
        help_text = """Vectron Python Client Help

Constructor:
- VectronClient(host, api_key=None, options=None)

Options (ClientOptions):
- timeout_seconds: per-RPC timeout in seconds (default 10.0, <= 0 disables)
- use_tls: enable TLS (default False)
- tls_root_certificates: custom root certs (bytes, optional)
- tls_server_name: TLS server name override (optional)
- max_recv_message_length: max inbound bytes (default 16MB)
- max_send_message_length: max outbound bytes (default 16MB)
- keepalive_time_ms: keepalive ping interval (default 30000)
- keepalive_timeout_ms: keepalive ack timeout (default 10000)
- keepalive_permit_without_calls: allow pings without active RPCs (default False)
- expected_vector_dim: validate vector length when > 0 (default 0, disabled)
- compression: gRPC compression (default None, set "gzip")
- hedged_reads: enable duplicate reads to reduce tail latency (default False)
- hedge_delay_ms: delay before hedged read (default 75)
- max_batch_bytes: max batch payload size in bytes (approximate, default 0)
- retry_max_attempts: max attempts including first (default 3)
- retry_initial_backoff_ms: base backoff (default 100)
- retry_max_backoff_ms: max backoff (default 2000)
- retry_backoff_multiplier: exponential factor (default 2.0)
- retry_jitter: random jitter fraction (default 0.2)
- retry_on_writes: retry non-idempotent ops (default False)
- retryable_statuses: list of retryable grpc.StatusCode (optional)

Safety defaults:
- Timeouts enabled
- Message sizes capped
- Optional vector dimension validation
- Retries enabled for read-only operations only
- Hedged reads disabled by default

Performance defaults:
- Reused gRPC channel
- Keepalive enabled with conservative intervals
"""

        options = {
            "timeout_seconds": 10.0,
            "use_tls": False,
            "tls_root_certificates": None,
            "tls_server_name": None,
            "max_recv_message_length": 16 * 1024 * 1024,
            "max_send_message_length": 16 * 1024 * 1024,
            "keepalive_time_ms": 30000,
            "keepalive_timeout_ms": 10000,
            "keepalive_permit_without_calls": False,
            "expected_vector_dim": 0,
            "compression": None,
            "hedged_reads": False,
            "hedge_delay_ms": 75,
            "max_batch_bytes": 0,
            "retry_max_attempts": 3,
            "retry_initial_backoff_ms": 100,
            "retry_max_backoff_ms": 2000,
            "retry_backoff_multiplier": 2.0,
            "retry_jitter": 0.2,
            "retry_on_writes": False,
            "retryable_statuses": ["UNAVAILABLE", "DEADLINE_EXCEEDED", "RESOURCE_EXHAUSTED"],
            "notes": {
                "timeout_disable": "Set timeout_seconds <= 0 to disable per-RPC deadlines.",
                "tls": "Use TLS in production for secure transport.",
                "retries": "Retries apply to read-only operations unless retry_on_writes is enabled.",
                "hedged_reads": "Hedged reads can reduce tail latency but may increase load.",
            },
        }
        return help_text, options


class VectronClientPool:
    """Simple round-robin pool of clients for high-QPS usage."""

    def __init__(self, host: str, api_key: str = None, options: Optional[ClientOptions] = None, size: int = 1):
        if size <= 0:
            size = 1
        self._clients = [VectronClient(host, api_key=api_key, options=options) for _ in range(size)]
        self._lock = threading.Lock()
        self._index = 0

    def next(self) -> VectronClient:
        with self._lock:
            client = self._clients[self._index]
            self._index = (self._index + 1) % len(self._clients)
            return client

    def close(self):
        for client in self._clients:
            client.close()
