export class VectronError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "VectronError";
  }
}

export class AuthenticationError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "AuthenticationError";
  }
}

export class NotFoundError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "NotFoundError";
  }
}

export class InvalidArgumentError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "InvalidArgumentError";
  }
}

export class AlreadyExistsError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "AlreadyExistsError";
  }
}

export class InternalServerError extends VectronError {
  constructor(message: string) {
    super(message);
    this.name = "InternalServerError";
  }
}
