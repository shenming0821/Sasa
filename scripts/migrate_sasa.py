#!/usr/bin/env python3
"""Migrate Fuyu data directories to Sasa names.

Run once after installing the renamed build.  Moves
  ~/Library/Application Support/Fuyu   ->  .../Sasa
  ~/Library/Application Support/Fuyu Preview -> .../Sasa Preview
  ~/Library/Caches/Fuyu                ->  .../Sasa
  ~/Library/Logs/Fuyu                  ->  .../Sasa
and renames the database files inside (fuyu.db -> sasa.db, fuyu.lock -> sasa.lock).
Existing Sasa directories are left untouched; already-migrated files are skipped.
"""

import os
import shutil
import sys
from pathlib import Path

HOME = Path.home()
SUPPORT = HOME / 'Library/Application Support'
CACHE = HOME / 'Library/Caches'
LOGS = HOME / 'Library/Logs'

RENAMES = [
    (SUPPORT / 'Fuyu', SUPPORT / 'Sasa'),
    (SUPPORT / 'Fuyu Preview', SUPPORT / 'Sasa Preview'),
    (CACHE / 'Fuyu', CACHE / 'Sasa'),
    (LOGS / 'Fuyu', LOGS / 'Sasa'),
]

DB_RENAMES = {
    'fuyu.db': 'sasa.db',
    'fuyu.db-wal': 'sasa.db-wal',
    'fuyu.db-shm': 'sasa.db-shm',
    'fuyu.lock': 'sasa.lock',
    'fuyu.sqlite': 'sasa.sqlite',
}


def migrate_tree(source: Path, target: Path) -> None:
    if not source.exists():
        return
    if target.exists():
        for child in source.iterdir():
            dest = target / child.name
            if dest.exists():
                print(f'  skip existing {dest}')
                continue
            shutil.move(str(child), str(dest))
        source.rmdir()
        print(f'merged {source} -> {target}')
    else:
        shutil.move(str(source), str(target))
        print(f'moved {source} -> {target}')


def rename_db_files(directory: Path) -> None:
    if not directory.is_dir():
        return
    for old_name, new_name in DB_RENAMES.items():
        old = directory / old_name
        new = directory / new_name
        if old.exists() and not new.exists():
            old.rename(new)
            print(f'  renamed {old.name} -> {new.name}')


def main() -> int:
    for source, target in RENAMES:
        migrate_tree(source, target)
    for _, target in RENAMES:
        rename_db_files(target)
    print('migration complete')
    return 0


if __name__ == '__main__':
    sys.exit(main())
