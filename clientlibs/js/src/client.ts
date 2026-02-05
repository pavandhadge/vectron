/**
 * This file implements the TypeScript client for the Vectron vector database.
 * It provides a user-friendly, async/await-based interface for interacting
 * with the Vectron API, handling gRPC connection, authentication, and error translation.
 */
import * as grpc from "@grpc/grpc-js";

import {
  VectronError,
  AuthenticationError,
  NotFoundError,
  InvalidArgumentError,
  AlreadyExistsError,
  InternalServerError,
} from "./errors";

// Import the generated protobuf client and message types.
import {
  CreateCollectionRequest,
  VectronServiceClient,
  DeleteRequest,
  GetCollectionStatusRequest,
  GetCollectionStatusResponse,
  GetRequest,
  ListCollectionsRequest,
  Point as ProtoPoint,
  SearchRequest,
  UpsertRequest,
} from "../proto/apigateway/apigateway";

/**
 * Represents a single vector point for upsert operations.
 */
export interface Point {
  id: string;
  vector: number[];
  payload?: Record<string, string>;
}

/**
 * Represents a single search result, including its ID, score, and payload.
 */
export interface SearchResult {
  id: string;
  score: number;
  payload?: Record<string, string>;
}

/**
 * Configuration options for the Vectron client.
 */
export interface ClientOptions {
  timeoutMs?: number;
  useTLS?: boolean;
  tlsRootCerts?: Buffer;
  tlsServerName?: string;
  maxRecvMsgSize?: number;
  maxSendMsgSize?: number;
  keepaliveTimeMs?: number;
  keepaliveTimeoutMs?: number;
  keepalivePermitWithoutCalls?: boolean;
  expectedVectorDim?: number;
  userAgent?: string;
  compression?: "gzip";
  hedgedReads?: boolean;
  hedgeDelayMs?: number;
  maxBatchBytes?: number;
  retryMaxAttempts?: number;
  retryInitialBackoffMs?: number;
  retryMaxBackoffMs?: number;
  retryBackoffMultiplier?: number;
  retryJitter?: number;
  retryOnWrites?: boolean;
  retryableStatusCodes?: number[];
}

const defaultOptions: Required<Omit<ClientOptions, "tlsRootCerts" | "tlsServerName">> &
  Pick<ClientOptions, "tlsRootCerts" | "tlsServerName"> = {
  timeoutMs: 10000,
  useTLS: false,
  tlsRootCerts: undefined,
  tlsServerName: undefined,
  maxRecvMsgSize: 16 * 1024 * 1024,
  maxSendMsgSize: 16 * 1024 * 1024,
  keepaliveTimeMs: 30000,
  keepaliveTimeoutMs: 10000,
  keepalivePermitWithoutCalls: false,
  expectedVectorDim: 0,
  userAgent: "vectron-js-client",
  compression: undefined,
  hedgedReads: false,
  hedgeDelayMs: 75,
  maxBatchBytes: 0,
  retryMaxAttempts: 3,
  retryInitialBackoffMs: 100,
  retryMaxBackoffMs: 2000,
  retryBackoffMultiplier: 2,
  retryJitter: 0.2,
  retryOnWrites: false,
  retryableStatusCodes: undefined,
};

function normalizeOptions(options?: ClientOptions) {
  return { ...defaultOptions, ...(options ?? {}) };
}

/**
 * The main client for interacting with the Vectron vector database.
 */
export class VectronClient {
  private readonly client: VectronServiceClient;
  private readonly apiKey?: string;
  private readonly options: ReturnType<typeof normalizeOptions>;

  /**
   * Initializes the Vectron client.
   * @param host The address of the apigateway gRPC endpoint (e.g., 'localhost:8081').
   * @param apiKey The API key for authentication.
   * @param options Optional client configuration for safety/performance.
   */
  constructor(host: string, apiKey?: string, options?: ClientOptions) {
    if (!host) {
      throw new InvalidArgumentError("Host cannot be empty.");
    }
    this.options = normalizeOptions(options);
    const channelOptions: grpc.ChannelOptions = {};
    if (this.options.maxRecvMsgSize) {
      channelOptions["grpc.max_receive_message_length"] =
        this.options.maxRecvMsgSize;
    }
    if (this.options.maxSendMsgSize) {
      channelOptions["grpc.max_send_message_length"] =
        this.options.maxSendMsgSize;
    }
    if (this.options.keepaliveTimeMs) {
      channelOptions["grpc.keepalive_time_ms"] = this.options.keepaliveTimeMs;
    }
    if (this.options.keepaliveTimeoutMs) {
      channelOptions["grpc.keepalive_timeout_ms"] =
        this.options.keepaliveTimeoutMs;
    }
    channelOptions["grpc.keepalive_permit_without_calls"] =
      this.options.keepalivePermitWithoutCalls ? 1 : 0;
    if (this.options.userAgent) {
      channelOptions["grpc.primary_user_agent"] = this.options.userAgent;
    }
    if (this.options.tlsServerName) {
      channelOptions["grpc.ssl_target_name_override"] =
        this.options.tlsServerName;
    }

    const creds = this.options.useTLS
      ? grpc.credentials.createSsl(this.options.tlsRootCerts)
      : grpc.credentials.createInsecure();

    this.client = new VectronServiceClient(host, creds, channelOptions);
    this.apiKey = apiKey;
  }

  /**
   * Creates the metadata for a gRPC request, including the authorization header.
   */
  private getMetadata(): grpc.Metadata {
    const meta = new grpc.Metadata();
    if (this.apiKey) {
      meta.add("authorization", `Bearer ${this.apiKey}`);
    }
    return meta;
  }

  private getCallOptions(): grpc.CallOptions {
    const callOptions: grpc.CallOptions = {};
    if (this.options.timeoutMs && this.options.timeoutMs > 0) {
      callOptions.deadline = new Date(Date.now() + this.options.timeoutMs);
    }
    if (this.options.compression === "gzip") {
      callOptions.compression = grpc.CompressionAlgorithms.gzip;
    }
    return callOptions;
  }

  private validateVectorDim(vector: number[]) {
    if (this.options.expectedVectorDim && vector.length !== this.options.expectedVectorDim) {
      throw new InvalidArgumentError(
        `Vector dimension ${vector.length} does not match expected ${this.options.expectedVectorDim}.`,
      );
    }
  }

  private getRetryableCodes(): number[] {
    if (this.options.retryableStatusCodes && this.options.retryableStatusCodes.length > 0) {
      return this.options.retryableStatusCodes;
    }
    return [
      grpc.status.UNAVAILABLE,
      grpc.status.DEADLINE_EXCEEDED,
      grpc.status.RESOURCE_EXHAUSTED,
    ];
  }

  private shouldRetry(err: unknown, isWrite: boolean, attempt: number): boolean {
    const maxAttempts = this.options.retryMaxAttempts ?? 1;
    if (maxAttempts <= 1 || attempt >= maxAttempts) {
      return false;
    }
    if (isWrite && !this.options.retryOnWrites) {
      return false;
    }
    const svcErr = err as grpc.ServiceError;
    if (svcErr?.code === undefined) {
      return false;
    }
    return this.getRetryableCodes().includes(svcErr.code);
  }

  private computeBackoffMs(attempt: number): number {
    let backoff = Math.max(this.options.retryInitialBackoffMs ?? 100, 1);
    const multiplier = Math.max(this.options.retryBackoffMultiplier ?? 2, 2);
    for (let i = 1; i < attempt; i += 1) {
      backoff = Math.floor(backoff * multiplier);
      if (this.options.retryMaxBackoffMs && backoff > this.options.retryMaxBackoffMs) {
        backoff = this.options.retryMaxBackoffMs;
        break;
      }
    }
    const jitter = this.options.retryJitter ?? 0;
    if (jitter > 0) {
      const delta = (Math.random() * 2 - 1) * jitter;
      backoff = Math.max(0, Math.floor(backoff * (1 + delta)));
    }
    return backoff;
  }

  private async sleep(ms: number): Promise<void> {
    await new Promise((resolve) => setTimeout(resolve, ms));
  }

  private async callWithRetry<T>(isWrite: boolean, fn: () => Promise<T>): Promise<T> {
    const maxAttempts = this.options.retryMaxAttempts ?? 1;
    let attempt = 1;
    while (true) {
      try {
        return await fn();
      } catch (err) {
        if (!this.shouldRetry(err, isWrite, attempt)) {
          if (err instanceof VectronError) {
            throw err;
          }
          const svcErr = err as grpc.ServiceError;
          if (svcErr?.code === undefined) {
            throw err as Error;
          }
          throw this._handleError(svcErr);
        }
        await this.sleep(this.computeBackoffMs(attempt));
        attempt += 1;
        if (attempt > maxAttempts) {
          if (err instanceof VectronError) {
            throw err;
          }
          const svcErr = err as grpc.ServiceError;
          if (svcErr?.code === undefined) {
            throw err as Error;
          }
          throw this._handleError(svcErr);
        }
      }
    }
  }

  private async callRead<T>(fn: () => Promise<T>): Promise<T> {
    if (!this.options.hedgedReads) {
      return this.callWithRetry(false, fn);
    }
    const hedgeDelay = Math.max(this.options.hedgeDelayMs ?? 75, 0);
    return this.callWithRetry(false, () => this.hedged(fn, hedgeDelay));
  }

  private async hedged<T>(fn: () => Promise<T>, delayMs: number): Promise<T> {
    let settled = false;
    let lastError: unknown;
    let resolveOut: (value: T) => void;
    let rejectOut: (reason?: unknown) => void;

    const out = new Promise<T>((resolve, reject) => {
      resolveOut = resolve;
      rejectOut = reject;
    });

    const run = async () => {
      try {
        const res = await fn();
        if (!settled) {
          settled = true;
          resolveOut(res);
        }
      } catch (err) {
        lastError = err;
        if (settled) {
          return;
        }
        if (delayMs === 0) {
          settled = true;
          rejectOut(err);
        }
      }
    };

    run();
    if (delayMs > 0) {
      setTimeout(() => {
        if (!settled) {
          run().catch(() => undefined);
        }
      }, delayMs);
    }

    try {
      return await out;
    } catch (err) {
      throw (err ?? lastError) as Error;
    }
  }

  /**
   * Translates a gRPC ServiceError into a specific VectronError subclass.
   */
  private _handleError(err: grpc.ServiceError): Error {
    switch (err.code) {
      case grpc.status.UNAUTHENTICATED:
        return new AuthenticationError(err.details);
      case grpc.status.NOT_FOUND:
        return new NotFoundError(err.details);
      case grpc.status.INVALID_ARGUMENT:
        return new InvalidArgumentError(err.details);
      case grpc.status.ALREADY_EXISTS:
        return new AlreadyExistsError(err.details);
      case grpc.status.INTERNAL:
        return new InternalServerError(err.details);
      default:
        return new VectronError(
          `An unexpected gRPC error occurred: ${err.details}`,
        );
    }
  }

  /**
   * Creates a new collection in Vectron.
   * @param name The name of the collection. Must be a non-empty string.
   * @param dimension The dimension of the vectors that will be stored in this collection.
   * @param distance The distance metric to use ('euclidean', 'cosine', or 'dot').
   * @throws {InvalidArgumentError} If parameters are invalid.
   * @throws {AlreadyExistsError} If a collection with the same name already exists.
   * @throws {VectronError} For other server-side or connection errors.
   */
  public async createCollection(
    name: string,
    dimension: number,
    distance: string = "euclidean",
  ): Promise<void> {
    if (!name) {
      throw new InvalidArgumentError("Collection name cannot be empty.");
    }
    if (dimension <= 0) {
      throw new InvalidArgumentError("Dimension must be a positive integer.");
    }

    const request = CreateCollectionRequest.create({
      name,
      dimension,
      distance,
    });

    return this.callWithRetry(true, () => {
      return new Promise((resolve, reject) => {
        this.client.createCollection(
          request,
          this.getMetadata(),
          this.getCallOptions(),
          (err, _response) => {
            if (err) {
              return reject(err);
            }
            resolve();
          },
        );
      });
    });
  }

  /**
   * Retrieves a list of all collection names.
   * @returns A promise that resolves with a list of collection names.
   * @throws {VectronError} For server-side or connection errors.
   */
  public async listCollections(): Promise<string[]> {
    const request = ListCollectionsRequest.create();
    return this.callRead(() => {
      return new Promise((resolve, reject) => {
        this.client.listCollections(
          request,
          this.getMetadata(),
          this.getCallOptions(),
          (err, response) => {
            if (err) {
              return reject(err);
            }
            resolve(response.collections);
          },
        );
      });
    });
  }

  /**
   * Retrieves the status of a collection, including the readiness of its shards.
   * @param name The name of the collection.
   * @returns A promise that resolves with the collection status.
   * @throws {InvalidArgumentError} If the collection name is empty.
   * @throws {NotFoundError} If the collection does not exist.
   * @throws {VectronError} For other server-side or connection errors.
   */
  public async getCollectionStatus(
    name: string,
  ): Promise<GetCollectionStatusResponse> {
    if (!name) {
      throw new InvalidArgumentError("Collection name cannot be empty.");
    }

    const request = GetCollectionStatusRequest.create({ name });

    return this.callRead(() => {
      return new Promise((resolve, reject) => {
        this.client.getCollectionStatus(
          request,
          this.getMetadata(),
          this.getCallOptions(),
          (err, response) => {
            if (err) {
              return reject(err);
            }
            resolve(response);
          },
        );
      });
    });
  }

  /**
   * Inserts or updates one or more vectors in a specified collection.
   * @param collection The name of the collection to upsert into.
   * @param points An array of `Point` objects.
   * @returns A promise that resolves with the number of points successfully upserted.
   * @throws {InvalidArgumentError} If parameters are invalid.
   * @throws {NotFoundError} If the specified collection does not exist.
   * @throws {VectronError} For other server-side or connection errors.
   */
  public async upsert(collection: string, points: Point[]): Promise<number> {
    if (!collection) {
      throw new InvalidArgumentError("Collection name cannot be empty.");
    }
    if (!points || points.length === 0) {
      return 0;
    }

    const protoPoints = points.map((p) => {
      if (!p.id) {
        throw new InvalidArgumentError("Point ID cannot be empty.");
      }
      if (!p.vector || p.vector.length === 0) {
        throw new InvalidArgumentError("Point vector cannot be empty.");
      }
      this.validateVectorDim(p.vector);
      return ProtoPoint.create({
        id: p.id,
        vector: p.vector,
        payload: p.payload,
      });
    });

    const request = UpsertRequest.create({ collection, points: protoPoints });

    return this.callWithRetry(true, () => {
      return new Promise((resolve, reject) => {
        this.client.upsert(
          request,
          this.getMetadata(),
          this.getCallOptions(),
          (err, response) => {
            if (err) {
              return reject(err);
            }
            resolve(response.upserted);
          },
        );
      });
    });
  }

  /**
   * Searches a collection for the k vectors most similar to the query vector.
   * @param collection The name of the collection to search in.
   * @param vector The query vector.
   * @param topK The number of nearest neighbors to return.
   * @returns A promise that resolves with a list of `SearchResult` objects.
   * @throws {InvalidArgumentError} If parameters are invalid.
   * @throws {NotFoundError} If the specified collection does not exist.
   * @throws {VectronError} For other server-side or connection errors.
   */
  public async search(
    collection: string,
    vector: number[],
    topK: number = 10,
  ): Promise<SearchResult[]> {
    if (!collection) {
      throw new InvalidArgumentError("Collection name cannot be empty.");
    }
    if (!vector || vector.length === 0) {
      throw new InvalidArgumentError("Query vector cannot be empty.");
    }
    this.validateVectorDim(vector);

    const request = SearchRequest.create({ collection, vector, topK });

    return this.callRead(() => {
      return new Promise((resolve, reject) => {
        this.client.search(
          request,
          this.getMetadata(),
          this.getCallOptions(),
          (err, response) => {
            if (err) {
              return reject(err);
            }
            resolve(
              response.results.map((r) => ({
                id: r.id,
                score: r.score,
                payload: r.payload,
              })),
            );
          },
        );
      });
    });
  }

  /**
   * Retrieves a single point by its ID from a collection.
   * @param collection The name of the collection.
   * @param pointId The ID of the point to retrieve.
   * @returns A promise that resolves with the requested `Point` object.
   * @throws {InvalidArgumentError} If parameters are invalid.
   * @throws {NotFoundError} If the collection or point does not exist.
   * @throws {VectronError} For other server-side or connection errors.
   */
  public async get(collection: string, pointId: string): Promise<Point> {
    if (!collection || !pointId) {
      throw new InvalidArgumentError(
        "Collection name and point ID cannot be empty.",
      );
    }

    const request = GetRequest.create({ collection, id: pointId });

    return this.callRead(() => {
      return new Promise((resolve, reject) => {
        this.client.get(
          request,
          this.getMetadata(),
          this.getCallOptions(),
          (err, response) => {
            if (err) {
              return reject(err);
            }
            const point = response.point;
            if (!point) {
              return reject(
                new NotFoundError(`Point with id ${pointId} not found.`),
              );
            }
            resolve({
              id: point.id,
              vector: point.vector,
              payload: point.payload,
            });
          },
        );
      });
    });
  }

  /**
   * Deletes a single point by its ID from a collection.
   * @param collection The name of the collection.
   * @param pointId The ID of the point to delete.
   * @throws {InvalidArgumentError} If parameters are invalid.
   * @throws {NotFoundError} If the collection or point does not exist.
   * @throws {VectronError} For other server-side or connection errors.
   */
  public async delete(collection: string, pointId: string): Promise<void> {
    if (!collection || !pointId) {
      throw new InvalidArgumentError(
        "Collection name and point ID cannot be empty.",
      );
    }

    const request = DeleteRequest.create({ collection, id: pointId });

    return this.callWithRetry(true, () => {
      return new Promise((resolve, reject) => {
        this.client.delete(
          request,
          this.getMetadata(),
          this.getCallOptions(),
          (err, _response) => {
            if (err) {
              return reject(err);
            }
            resolve();
          },
        );
      });
    });
  }

  /**
   * Closes the underlying gRPC client connection.
   */
  public close() {
    this.client.close();
  }

  /**
   * Splits large upserts into batches for higher throughput.
   */
  public async upsertBatch(
    collection: string,
    points: Point[],
    batchSize: number = 256,
    concurrency: number = 1,
  ): Promise<number> {
    if (batchSize <= 0) {
      batchSize = 256;
    }
    if (concurrency <= 0) {
      concurrency = 1;
    }
    if (!points || points.length === 0) {
      return 0;
    }
    const estimatePointBytes = (p: Point): number => {
      let size = p.id.length + p.vector.length * 4;
      if (p.payload) {
        for (const [k, v] of Object.entries(p.payload)) {
          size += k.length + v.length;
        }
      }
      return size + 16;
    };

    const batches: Point[][] = [];
    const maxBatchBytes = this.options.maxBatchBytes ?? 0;
    if (maxBatchBytes > 0) {
      let idx = 0;
      while (idx < points.length) {
        const batch: Point[] = [];
        let bytes = 0;
        while (idx < points.length && batch.length < batchSize) {
          const p = points[idx];
          const pBytes = estimatePointBytes(p);
          if (batch.length === 0 || bytes + pBytes <= maxBatchBytes) {
            batch.push(p);
            bytes += pBytes;
            idx += 1;
            continue;
          }
          break;
        }
        batches.push(batch);
      }
    } else {
      for (let i = 0; i < points.length; i += batchSize) {
        batches.push(points.slice(i, i + batchSize));
      }
    }
    if (concurrency === 1) {
      let total = 0;
      for (const batch of batches) {
        total += await this.upsert(collection, batch);
      }
      return total;
    }
    const workers: Promise<number>[] = [];
    for (let i = 0; i < concurrency; i += 1) {
      const shard = batches.filter((_, idx) => idx % concurrency === i);
      workers.push(
        (async () => {
          let subtotal = 0;
          for (const batch of shard) {
            subtotal += await this.upsert(collection, batch);
          }
          return subtotal;
        })(),
      );
    }
    const results = await Promise.all(workers);
    return results.reduce((a, b) => a + b, 0);
  }

  /**
   * Returns a human-readable help string and a structured options map.
   */
  public static help(): { text: string; options: ClientOptions } {
    const text = `Vectron JS/TS Client Help

Constructor:
- new VectronClient(host, apiKey?, options?)

Options (ClientOptions):
- timeoutMs: per-RPC timeout in ms (default 10000, <= 0 disables)
- useTLS: enable TLS (default false)
- tlsRootCerts: custom root certs (Buffer, optional)
- tlsServerName: TLS server name override (optional)
- maxRecvMsgSize: max inbound bytes (default 16MB)
- maxSendMsgSize: max outbound bytes (default 16MB)
- keepaliveTimeMs: keepalive ping interval (default 30000)
- keepaliveTimeoutMs: keepalive ack timeout (default 10000)
- keepalivePermitWithoutCalls: allow pings without active RPCs (default false)
- expectedVectorDim: validate vector length when > 0 (default 0, disabled)
- userAgent: custom gRPC user agent (default "vectron-js-client")
- compression: gRPC compression (default disabled, set "gzip")
- hedgedReads: enable duplicate reads to reduce tail latency (default false)
- hedgeDelayMs: delay before hedged read (default 75)
- maxBatchBytes: max batch payload size in bytes (approximate, default 0)
- retryMaxAttempts: max attempts including first (default 3)
- retryInitialBackoffMs: base backoff (default 100)
- retryMaxBackoffMs: max backoff (default 2000)
- retryBackoffMultiplier: exponential factor (default 2)
- retryJitter: random jitter fraction (default 0.2)
- retryOnWrites: retry non-idempotent ops (default false)
- retryableStatusCodes: override retryable grpc.status codes (optional)

Safety defaults:
- Timeouts enabled
- Message sizes capped
- Optional vector dimension validation
- Retries enabled for read-only operations only
- Hedged reads disabled by default

Performance defaults:
- Reused gRPC channel
- Keepalive enabled with conservative intervals
`;
    return { text, options: { ...defaultOptions } };
  }
}

/**
 * Simple round-robin client pool for high-QPS usage.
 */
export class VectronClientPool {
  private readonly clients: VectronClient[];
  private index = 0;

  constructor(host: string, apiKey?: string, options?: ClientOptions, size: number = 1) {
    const count = size > 0 ? size : 1;
    this.clients = Array.from({ length: count }, () => new VectronClient(host, apiKey, options));
  }

  public next(): VectronClient {
    const client = this.clients[this.index];
    this.index = (this.index + 1) % this.clients.length;
    return client;
  }

  public close() {
    for (const client of this.clients) {
      client.close();
    }
  }
}
