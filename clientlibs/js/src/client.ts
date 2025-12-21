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
 * The main client for interacting with the Vectron vector database.
 */
export class VectronClient {
  private readonly client: VectronServiceClient;
  private readonly apiKey?: string;

  /**
   * Initializes the Vectron client.
   * @param host The address of the apigateway gRPC endpoint (e.g., 'localhost:8081').
   * @param apiKey The API key for authentication.
   */
  constructor(host: string, apiKey?: string) {
    if (!host) {
      throw new InvalidArgumentError("Host cannot be empty.");
    }
    // For production, use grpc.credentials.createSsl()
    this.client = new VectronServiceClient(
      host,
      grpc.credentials.createInsecure(),
    );
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

    return new Promise((resolve, reject) => {
      this.client.createCollection(
        request,
        this.getMetadata(),
        (err, _response) => {
          if (err) {
            return reject(this._handleError(err));
          }
          resolve();
        },
      );
    });
  }

  /**
   * Retrieves a list of all collection names.
   * @returns A promise that resolves with a list of collection names.
   * @throws {VectronError} For server-side or connection errors.
   */
  public async listCollections(): Promise<string[]> {
    const request = ListCollectionsRequest.create();
    return new Promise((resolve, reject) => {
      this.client.listCollections(
        request,
        this.getMetadata(),
        (err, response) => {
          if (err) {
            return reject(this._handleError(err));
          }
          resolve(response.collections);
        },
      );
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
      return ProtoPoint.create({
        id: p.id,
        vector: p.vector,
        payload: p.payload,
      });
    });

    const request = UpsertRequest.create({ collection, points: protoPoints });

    return new Promise((resolve, reject) => {
      this.client.upsert(request, this.getMetadata(), (err, response) => {
        if (err) {
          return reject(this._handleError(err));
        }
        resolve(response.upserted);
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

    const request = SearchRequest.create({ collection, vector, topK });

    return new Promise((resolve, reject) => {
      this.client.search(request, this.getMetadata(), (err, response) => {
        if (err) {
          return reject(this._handleError(err));
        }
        resolve(
          response.results.map((r) => ({
            id: r.id,
            score: r.score,
            payload: r.payload,
          })),
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

    return new Promise((resolve, reject) => {
      this.client.get(request, this.getMetadata(), (err, response) => {
        if (err) {
          return reject(this._handleError(err));
        }
        const point = response.point;
        if (!point) {
          // This case should ideally be handled by a NOT_FOUND error,
          // but as a safeguard, we reject here.
          return reject(
            new NotFoundError(`Point with id ${pointId} not found.`),
          );
        }
        resolve({
          id: point.id,
          vector: point.vector,
          payload: point.payload,
        });
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

    return new Promise((resolve, reject) => {
      this.client.delete(request, this.getMetadata(), (err, _response) => {
        if (err) {
          return reject(this._handleError(err));
        }
        resolve();
      });
    });
  }

  /**
   * Closes the underlying gRPC client connection.
   */
  public close() {
    this.client.close();
  }
}
