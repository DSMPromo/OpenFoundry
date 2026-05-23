"""openfoundry-models — the model adapter SDK for OpenFoundry.

Importing this package registers every bundled adapter (the reference
``LinearModelAdapter`` and the four framework templates), so
:func:`load_model` can resolve any of them by ``adapter_id``.
"""

from .adapter import (
    Column,
    ModelAdapter,
    ModelApi,
    TabularShape,
    adapter_for,
    load_model,
    register,
    save_model,
)
from .frameworks import (
    HuggingFaceAdapter,
    PyTorchAdapter,
    SklearnAdapter,
    XGBoostAdapter,
)
from .reference import LinearModelAdapter

__version__ = "0.1.0"

__all__ = [
    "Column",
    "ModelAdapter",
    "ModelApi",
    "TabularShape",
    "adapter_for",
    "load_model",
    "register",
    "save_model",
    "LinearModelAdapter",
    "SklearnAdapter",
    "XGBoostAdapter",
    "PyTorchAdapter",
    "HuggingFaceAdapter",
]
