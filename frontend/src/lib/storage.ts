export class StorageUnavailableError extends Error {
  constructor() {
    super(
      "Browser storage is unavailable. Enable site storage so your session and pending answers can be recovered safely.",
    );
  }
}

function access<T>(operation: (storage: Storage) => T): T {
  try {
    return operation(window.sessionStorage);
  } catch {
    throw new StorageUnavailableError();
  }
}

export const sessionPersistence: Pick<
  Storage,
  "getItem" | "setItem" | "removeItem"
> = {
  getItem: (key) => access((storage) => storage.getItem(key)),
  setItem: (key, value) => access((storage) => storage.setItem(key, value)),
  removeItem: (key) => access((storage) => storage.removeItem(key)),
};
