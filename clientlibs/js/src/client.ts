// First, import the gRPC module
import * as grpc from "@grpc/grpc-js";

// Then, import the custom error classes
import {
  VectronError,
  AuthenticationError,
  NotFoundError,
  InvalidArgumentError,
  AlreadyExistsError,
  InternalServerError,
} from "./errors";

// Finally, import the generated protobuf classes from the correct path
import {
  CreateCollectionRequest,
  VectronServiceClient,
  DeleteRequest,
  GetRequest,
  ListCollectionsRequest,
  Point as ProtoPoint,
  SearchRequest,
  UpsertRequest,
} from "../proto/apigateway/apigateway.ts";

/**
 * Represents a single vector point.
 */
export interface Point {
  id: string;
  vector: number[];
  payload?: Record<string, string>;
}

/**
 * Represents a single search result.
 */
export interface SearchResult {
  id: string;
  score: number;
  payload?: Record<string, string>;
}

/**
 * The TypeScript client for the Vectron vector database.
 */
export class VectronClient {
  private client: VectronServiceClient;
  private apiKey?: string;

  /**
   * Initializes the Vectron client.
   * @param host The address of the apigateway (e.g., 'localhost:8080').
   * @param apiKey The API key for authentication.
   */
  constructor(host: string, apiKey?: string) {
    if (!host) {
      throw new InvalidArgumentError("Host cannot be empty.");
    }
    this.client = new VectronServiceClient(
      host,
      grpc.credentials.createInsecure(),
    );
    this.apiKey = apiKey;
  }

  private getMetadata(): grpc.Metadata {
    const meta = new grpc.Metadata();
    if (this.apiKey) {
      meta.add("authorization", `Bearer ${this.apiKey}`);
    }
    return meta;
  }

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
        return new VectronError(err.details);
    }
  }

  /**
   * Creates a new collection.
   * @param name The name of the collection. Must be a non-empty string.
   * @param dimension The dimension of the vectors in the collection. Must be a positive integer.
   * @param distance The distance metric to use ('euclidean', 'cosine', or 'dot'). Defaults to 'euclidean'.
   * @throws {InvalidArgumentError} If parameters are invalid.
   * @throws {AlreadyExistsError} If the collection already exists.
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

    const request = new CreateCollectionRequest();
    request.setName(name);
    request.setDimension(dimension);
    request.setDistance(distance);

    return new Promise((resolve, reject) => {
      this.client.createCollection(
        request,
        this.getMetadata(),
        (err, response) => {
          if (err) {
            return reject(this._handleError(err));
          }
          resolve();
        },
      );
    });
  }

  /**
   * Lists all collections.
   * @returns A list of collection names.
   * @throws {VectronError} For server-side or connection errors.
   */
  public async listCollections(): Promise<string[]> {
    const request = new ListCollectionsRequest();
    return new Promise((resolve, reject) => {
      this.client.listCollections(
        request,
        this.getMetadata(),
        (err, response) => {
          if (err) {
            return reject(this._handleError(err));
          }
          resolve(response.getCollectionsList());
        },
      );
    });
  }

  /**
   * Inserts or updates vectors in a collection.
   * @param collection The name of the collection.
   * @param points A list of `Point` objects to upsert.
   * @returns The number of points upserted.
   * @throws {InvalidArgumentError} If parameters are invalid.
   * @throws {NotFoundError} If the collection does not exist.
   * @throws {VectronError} For other server-side or connection errors.
   */
  public async upsert(collection: string, points: Point[]): Promise<number> {
    if (!collection) {
      throw new InvalidArgumentError("Collection name cannot be empty.");
    }
    if (!points || points.length === 0) {
      return 0;
    }

    const request = new UpsertRequest();
    request.setCollection(collection);
    request.setPointsList(
      points.map((p) => {
        if (!p.id) {
          throw new InvalidArgumentError("Point ID cannot be empty.");
        }
        const protoPoint = new ProtoPoint();
        protoPoint.setId(p.id);
        protoPoint.setVectorList(p.vector);
        if (p.payload) {
          const payloadMap = protoPoint.getPayloadMap();
          for (const [key, value] of Object.entries(p.payload)) {
            payloadMap.set(key, value);
          }
        }
        return protoPoint;
      }),
    );

    return new Promise((resolve, reject) => {
      this.client.upsert(request, this.getMetadata(), (err, response) => {
        if (err) {
          return reject(this._handleError(err));
        }
        resolve(response.getUpserted());
      });
    });
  }

  /**
   * Searches for the k nearest neighbors to a query vector.
   * @param collection The name of the collection.
   * @param vector The query vector.
   * @param topK The number of results to return.
   * @returns A list of `SearchResult` objects.
   * @throws {InvalidArgumentError} If parameters are invalid.
   * @throws {NotFoundError} If the collection does not exist.
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

    const request = new SearchRequest();
    request.setCollection(collection);
    request.setVectorList(vector);
    request.setTopK(topK);

    return new Promise((resolve, reject) => {
      this.client.search(request, this.getMetadata(), (err, response) => {
        if (err) {
          return reject(this._handleError(err));
        }
        resolve(
          response.getResultsList().map((r) => ({
            id: r.getId(),
            score: r.getScore(),
            payload: r.getPayloadMap().toObject(),
          })),
        );
      });
    });
  }

  /**
   * Retrieves a point by its ID.
   * @param collection The name of the collection.
   * @param pointId The ID of the point to retrieve.
   * @returns The `Point` object.
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

    const request = new GetRequest();
    request.setCollection(collection);
    request.setId(pointId);

    return new Promise((resolve, reject) => {
      this.client.get(request, this.getMetadata(), (err, response) => {
        if (err) {
          return reject(this._handleError(err));
        }
        const point = response.getPoint();
        resolve({
          id: point.getId(),
          vector: point.getVectorList(),
          payload: point.getPayloadMap().toObject(),
        });
      });
    });
  }

  /**
   * Deletes a point by its ID.
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

    const request = new DeleteRequest();
    request.setCollection(collection);
    request.setId(pointId);

    return new Promise((resolve, reject) => {
      this.client.delete(request, this.getMetadata(), (err, response) => {
        if (err) {
          return reject(this._handleError(err));
        }
        resolve();
      });
    });
  }

  /**
   * Closes the gRPC client connection.
   */
  public close() {
    this.client.close();
  }
}
