"""The OpenFoundry model adapter contract.

A :class:`ModelAdapter` is the open-source analogue of
``palantir_models.ModelAdapter``: it teaches the platform how to
serialise a trained model into a directory (the "model archive"), load
it back, declare its input/output schema, and run inference. Training,
the model registry, and live/batch deployments all consume models
through this single contract.
"""

from __future__ import annotations

import abc
import json
from dataclasses import asdict, dataclass, field
from pathlib import Path
from typing import Any

_METADATA_FILE = "openfoundry_model.json"


@dataclass
class Column:
    """A named, typed column of a model input or output."""

    name: str
    type: str = "string"
    description: str = ""


@dataclass
class TabularShape:
    """The columns of one named input or output table."""

    name: str
    columns: list[Column] = field(default_factory=list)


@dataclass
class ModelApi:
    """A model's declared inference contract.

    The platform uses it to validate datasets against the model and to
    generate the request/response schema of live and batch deployments.
    """

    inputs: list[TabularShape] = field(default_factory=list)
    outputs: list[TabularShape] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


class ModelAdapter(abc.ABC):
    """Base class for every model adapter.

    Subclasses implement four operations: :meth:`save` writes the model
    into a directory; :meth:`load` reconstructs an adapter from that
    directory; :meth:`predict` runs inference; :meth:`api` declares the
    input/output schema.

    ``adapter_id`` names the adapter kind in the archive metadata so
    :func:`load_model` can pick the right class. Register concrete
    adapters with :func:`register` so the dispatch can find them.
    """

    #: Stable identifier written into the archive metadata. Concrete
    #: adapters must override it with a unique value.
    adapter_id: str = "openfoundry.base"

    @abc.abstractmethod
    def save(self, path: Path) -> None:
        """Serialise the model into the directory ``path``."""

    @classmethod
    @abc.abstractmethod
    def load(cls, path: Path) -> "ModelAdapter":
        """Reconstruct an adapter of this class from ``path``."""

    @abc.abstractmethod
    def predict(self, data: Any) -> Any:
        """Run inference over ``data`` and return the predictions."""

    @abc.abstractmethod
    def api(self) -> ModelApi:
        """Declare the model's input/output schema."""


_REGISTRY: dict[str, type[ModelAdapter]] = {}


def register(cls: type[ModelAdapter]) -> type[ModelAdapter]:
    """Class decorator that registers an adapter under its ``adapter_id``."""

    if not cls.adapter_id or cls.adapter_id == ModelAdapter.adapter_id:
        raise ValueError(f"{cls.__name__} must declare a unique adapter_id")
    _REGISTRY[cls.adapter_id] = cls
    return cls


def adapter_for(adapter_id: str) -> type[ModelAdapter]:
    """Return the adapter class registered under ``adapter_id``."""

    try:
        return _REGISTRY[adapter_id]
    except KeyError:
        raise KeyError(
            f"no model adapter registered for {adapter_id!r}; "
            f"known adapters: {sorted(_REGISTRY)}"
        ) from None


def save_model(adapter: ModelAdapter, path: str | Path) -> Path:
    """Write ``adapter`` into the directory ``path`` as a model archive.

    The archive carries an ``openfoundry_model.json`` metadata file
    naming the adapter, so :func:`load_model` can round-trip it without
    the caller knowing the concrete class.
    """

    path = Path(path)
    path.mkdir(parents=True, exist_ok=True)
    (path / _METADATA_FILE).write_text(
        json.dumps({"adapter_id": adapter.adapter_id}, indent=2),
        encoding="utf-8",
    )
    adapter.save(path)
    return path


def load_model(path: str | Path) -> ModelAdapter:
    """Load a model archive written by :func:`save_model`."""

    path = Path(path)
    meta = json.loads((path / _METADATA_FILE).read_text(encoding="utf-8"))
    return adapter_for(meta["adapter_id"]).load(path)
