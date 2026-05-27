export interface IncludeDocumentFileState {
  mtimeMs: number;
  size: number;
}

interface CachedIncludeDocument<TDocument> extends IncludeDocumentFileState {
  parsed: TDocument;
}

interface PendingIncludeDocument<TDocument> extends IncludeDocumentFileState {
  promise: Promise<TDocument>;
}

export class IncludeDocumentLoader<TDocument> {
  private readonly cache = new Map<string, CachedIncludeDocument<TDocument>>();
  private readonly pending = new Map<string, PendingIncludeDocument<TDocument>>();
  private generation = 0;

  get(fileName: string, state: IncludeDocumentFileState): TDocument | undefined {
    const cached = this.cache.get(fileName);
    return cached && sameFileState(cached, state) ? cached.parsed : undefined;
  }

  set(fileName: string, state: IncludeDocumentFileState, parsed: TDocument): void {
    this.cache.set(fileName, { ...state, parsed });
  }

  async getOrLoad(
    fileName: string,
    state: IncludeDocumentFileState,
    load: () => Promise<TDocument>,
  ): Promise<TDocument> {
    const cached = this.get(fileName, state);
    if (cached) {
      return cached;
    }
    const pending = this.pending.get(fileName);
    if (pending && sameFileState(pending, state)) {
      return pending.promise;
    }

    const generation = this.generation;
    const promise = Promise.resolve()
      .then(load)
      .then((parsed) => {
        if (this.generation === generation && this.pending.get(fileName)?.promise === promise) {
          this.set(fileName, state, parsed);
        }
        return parsed;
      });
    this.pending.set(fileName, { ...state, promise });
    try {
      return await promise;
    } finally {
      if (this.pending.get(fileName)?.promise === promise) {
        this.pending.delete(fileName);
      }
    }
  }

  clear(): void {
    this.generation += 1;
    this.cache.clear();
    this.pending.clear();
  }
}

function sameFileState(left: IncludeDocumentFileState, right: IncludeDocumentFileState): boolean {
  return left.mtimeMs === right.mtimeMs && left.size === right.size;
}
