"""Custom exception classes for the Vectron client."""

class VectronError(Exception):
    """Base class for all Vectron client exceptions."""
    pass

class AuthenticationError(VectronError):
    """Raised for authentication errors."""
    pass

class NotFoundError(VectronError):
    """Raised when a resource is not found."""
    pass

class InvalidArgumentError(VectronError):
    """Raised for invalid arguments."""
    pass

class AlreadyExistsError(VectronError):
    """Raised when a resource already exists."""
    pass

class InternalServerError(VectronError):
    """Raised for internal server errors."""
    pass
