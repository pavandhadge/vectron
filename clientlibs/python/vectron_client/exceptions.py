"""
This module defines the custom exception classes for the Vectron client.
Having a custom exception hierarchy allows users of the client to easily
catch and handle specific errors that may occur during API calls.
"""

class VectronError(Exception):
    """Base class for all Vectron client exceptions."""
    pass

class AuthenticationError(VectronError):
    """Raised for authentication errors, such as an invalid or missing API key."""
    pass

class NotFoundError(VectronError):
    """Raised when a requested resource (e.g., a collection or point) is not found."""
    pass

class InvalidArgumentError(VectronError):
    """Raised when a request contains invalid arguments."""
    pass

class AlreadyExistsError(VectronError):
    """Raised when attempting to create a resource that already exists."""
    pass

class InternalServerError(VectronError):
    """Raised for unhandled errors on the server side."""
    pass
