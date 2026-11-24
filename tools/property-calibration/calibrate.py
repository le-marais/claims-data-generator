"""Reproduce the Property reporting diagnostics using only the Python standard library."""
import csv
import subprocess
import tempfile
from collections import defaultdict
from datetime import date, timedelta
from decimal import Decimal
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
YEARS = range(2017, 2027)
SEEDS = (71, 72, 73)


def read(folder, name):
    with (folder / name).open() as source:
        return list(csv.DictReader(source))


def quarter(day):
    return day.year * 4 + (day.month - 1) // 3


def snapshot(declarations, binders, cut, overrides=None):
    """Volume-weighted adjacent cumulative factors, using observed pairs only."""
    overrides = overrides or {}
    cells = defaultdict(int)
    for row in declarations:
        processed = overrides.get(row["batch_id"], date.fromisoformat(row["processed_date"]))
        year = int(binders[row["binder_id"]]["year_of_account"])
        if processed <= cut and year in YEARS:
            cells[year, quarter(processed) - 4 * year + 1] += int(row["amount_cents"])
    rows = {}
    for year in YEARS:
        total = 0
        row = []
        for age in range(1, quarter(cut) - 4 * year + 2):
            total += cells[year, age]
            row.append(total)
        if row:
            rows[year] = row
    factors = []
    for age in range(1, max(map(len, rows.values()))):
        eligible = [row for row in rows.values() if len(row) > age]
        denominator = sum(row[age - 1] for row in eligible)
        assert denominator > 0
        factors.append(sum(row[age] for row in eligible) / denominator)
    estimates = {}
    for year, row in rows.items():
        ultimate = row[-1]
        for factor in factors[len(row) - 1:]:
            ultimate *= factor
        estimates[year] = ultimate
    return rows, estimates


def run(seed):
    with tempfile.TemporaryDirectory() as tmp:
        folder = Path(tmp)
        subprocess.run(["go", "run", "./cmd/claimsgen", "generate",
                        "--config", "data/property.yaml", "--seed", str(seed),
                        "--start-year", "2017", "--years", "10", "--initial-book-size", "1200",
                        "--origin-basis", "binder", "--out", str(folder)],
                       cwd=ROOT, check=True, capture_output=True)
        binders = {r["binder_id"]: r for r in read(folder, "binders.csv")}
        links = {r["policy_id"]: r["binder_id"] for r in read(folder, "policy_binders.csv")}
        declarations = read(folder, "premium_declarations.csv")
        truth = defaultdict(int)
        for row in read(folder, "policies.csv"):
            year = int(binders[links[row["policy_id"]]]["year_of_account"])
            truth[year] += int(Decimal(row["premium"]) * 100)
        for cut in (date(2025, 12, 31), date(2026, 6, 30), date(2026, 9, 30), date(2026, 12, 31)):
            rows, estimates = snapshot(declarations, binders, cut)
            y = cut.year
            mature = [year for year in rows if y - year >= 4]
            assert all(rows[year][-1] == truth[year] for year in mature)
            assert 0 < rows[y][-1] < truth[y]
            available = sum(int(b["estimate_cents"]) for b in binders.values()
                            if int(b["year_of_account"]) == y and date.fromisoformat(b["inception"]) <= cut)
            print(f"| {seed} | {cut} | {len(mature)} | {rows[y][-1]/truth[y]:.1%} | "
                  f"{estimates[y]/truth[y]:.1%} | {available/truth[y]:.1%} |")
        if seed == SEEDS[0]:
            stress(declarations, binders, truth[2026])


def stress(declarations, binders, truth):
    # Hold the January coverholder's November 2026 batch across year end.
    batch = "20261-2026-11"
    affected = [r for r in declarations if r["batch_id"] == batch]
    original = date.fromisoformat(affected[0]["processed_date"])
    delayed = original + timedelta(days=30)
    assert original.year == 2026 and delayed.year == 2027
    for row in declarations:
        if int(row["original_declaration_id"]) in {int(r["declaration_id"]) for r in affected}:
            assert date.fromisoformat(row["processed_date"]) >= delayed
    for cut in (date(2026, 12, 31), date(2027, 3, 31)):
        base, base_cl = snapshot(declarations, binders, cut)
        held, held_cl = snapshot(declarations, binders, cut, {batch: delayed})
        print(f"Stress {batch}, {len(affected)} transactions, {original} to {delayed}, at {cut}: "
              f"observed cents {base[2026][-1]} -> {held[2026][-1]}, "
              f"CL/truth {base_cl[2026]/truth:.2%} -> {held_cl[2026]/truth:.2%}.")
        if cut.year == 2027:
            assert base[2026][-1] == held[2026][-1]
            # The delayed observations remain in their actual processing quarter.


if __name__ == "__main__":
    for selected_seed in SEEDS:
        run(selected_seed)
