/**
 * This file defines the custom error classes for the Vectron client.
 * Having a custom error hierarchy allows users of the client to easily
 * catch and handle specific errors that may occur during API calls.
 */

/**
 * Base class for all custom errors thrown by the Vectron client.
 */
export class VectronError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "VectronError";
  }
}

/**
 * Thrown for authentication errors, such as an invalid or missing API key.
 */
export class AuthenticationError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "AuthenticationError";
  }
}

/**
 * Thrown when a requested resource (e.g., a collection or point) is not found.
 */
export class NotFoundError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "NotFoundError";
  }
}

/**
 * Thrown when a request contains invalid arguments (e.g., an empty collection name).
 */
export class InvalidArgumentError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "InvalidArgumentError";
  }
}

/**
 * Thrown when attempting to create a resource that already exists.
 */
export class AlreadyExistsError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "AlreadyExistsError";
  }
}

/**
 * Thrown for unhandled errors on the server side.
 */
export class InternalServerError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "InternalServerError";
  }
}
