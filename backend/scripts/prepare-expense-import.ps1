param(
    [Parameter(Mandatory = $true)]
    [string]$MaintenanceCsv,
    [Parameter(Mandatory = $false)]
    [string]$SafetyCsv,
    [Parameter(Mandatory = $false)]
    [string]$HRCsv,
    [Parameter(Mandatory = $false)]
    [string]$AdministrativeCsv,
    [Parameter(Mandatory = $false)]
    [datetime]$SettleBefore,
    [Parameter(Mandatory = $false)]
    [switch]$ReplaceAllExpenses,
    [Parameter(Mandatory = $false)]
    [datetime]$AllowLegacyUnassignedTruckOwnerBefore,
    [Parameter(Mandatory = $true)]
    [string]$OutputSql
)

$ErrorActionPreference = 'Stop'
$spreadsheetId = '1VKSL8fyBRjwOWAUhHlHUrJtn2HOa53tpuTm95TNqll4'
$usCulture = [Globalization.CultureInfo]::GetCultureInfo('en-US')
$invariantCulture = [Globalization.CultureInfo]::InvariantCulture

function ConvertTo-SqlText {
    param(
        [AllowNull()]
        [object]$Value,
        [switch]$PreserveEmpty
    )

    if ($null -eq $Value) { return 'NULL' }
    $text = ([string]$Value).Trim()
    if (-not $PreserveEmpty -and $text.Length -eq 0) { return 'NULL' }
    return "'$(($text -replace "'", "''"))'"
}

function ConvertTo-SqlDate {
    param([AllowNull()][object]$Value)

    $text = (([string]$Value).Trim() -replace '\.', '/') -replace '/+', '/'
    if ($text.Length -eq 0) { return 'NULL' }

    $parts = $text -split '/'
    $formats = if ($parts.Count -eq 3 -and [int]$parts[0] -gt 12) {
        @('d/M/yyyy', 'dd/MM/yyyy')
    } else {
        @('M/d/yyyy', 'MM/dd/yyyy', 'M/d/yy', 'MM/dd/yy')
    }
    $parsed = [datetime]::MinValue
    if (-not [datetime]::TryParseExact(
        $text,
        [string[]]$formats,
        $usCulture,
        [Globalization.DateTimeStyles]::None,
        [ref]$parsed
    )) {
        throw "Unrecognized expense date '$Value'."
    }
    return "'$($parsed.ToString('yyyy-MM-dd', $invariantCulture))'"
}

function ConvertTo-SqlAmount {
    param([AllowNull()][object]$Value)

    $text = ([string]$Value).Trim()
    if ($text.Length -eq 0) { return 'NULL' }

    $parsed = [decimal]0
    if (-not [decimal]::TryParse(
        $text,
        [Globalization.NumberStyles]::Currency,
        $usCulture,
        [ref]$parsed
    )) {
        if ($text -match '^\d+\.\d{3}\.\d{2}$') {
            $lastDot = $text.LastIndexOf('.')
            $text = ($text.Substring(0, $lastDot) -replace '\.', '') + $text.Substring($lastDot)
        }
        if (-not [decimal]::TryParse(
            $text,
            [Globalization.NumberStyles]::Number,
            $invariantCulture,
            [ref]$parsed
        )) {
            throw "Unrecognized expense amount '$Value'."
        }
    }
    return $parsed.ToString('0.00', $invariantCulture)
}

function ConvertTo-SqlBoolean {
    param([AllowNull()][object]$Value)
    if (([string]$Value).Trim().Equals('TRUE', [StringComparison]::OrdinalIgnoreCase)) {
        return 'true'
    }
    return 'false'
}

function Get-ExpenseRows {
    param(
        [string]$Path,
        [string]$Sheet,
        [AllowNull()][string]$FixedCategory,
        [switch]$Maintenance
    )

    $rows = @(Import-Csv -LiteralPath $Path)
    for ($index = 0; $index -lt $rows.Count; $index++) {
        $row = $rows[$index]
        # Native MSERP entries mirrored back to the sheet already have a ledger ID.
        # Keep their source row positions, but never reimport them as new expenses.
        if ($row.'MSERP Entry ID') {
            $exportID = [guid]::Empty
            if (-not [guid]::TryParse(([string]$row.'MSERP Entry ID').Trim(), [ref]$exportID)) {
                throw "Invalid MSERP Entry ID at $Sheet row $($index + 2)."
            }
            continue
        }
        if (-not (
            $row.Date -or $row.Amount -or $row.Description -or
            $row.'Expense Type' -or $row.Driver -or $row.'Unit #'
        )) {
            continue
        }

        $category = if ($FixedCategory) { $FixedCategory } else { ([string]$row.Category).Trim() }
        if ($category.Length -eq 0) { $category = 'Other' }

        [pscustomobject]@{
            Category = ConvertTo-SqlText $category
            ExpenseDate = ConvertTo-SqlDate $row.Date
            UnitNumber = ConvertTo-SqlText $row.'Unit #'
            DriverName = ConvertTo-SqlText $row.Driver
            Amount = ConvertTo-SqlAmount $row.Amount
            PaymentType = ConvertTo-SqlText $row.'Payment Type'
            ExpenseType = ConvertTo-SqlText $row.'Expense Type'
            ReferenceNumber = ConvertTo-SqlText $row.'Reference #'
            Description = ConvertTo-SqlText $row.Description
            CoveredBy = ConvertTo-SqlText $row.'Who will cover'
            PaidBy = ConvertTo-SqlText $row.'Paid By'
            ManagerVerified = ConvertTo-SqlBoolean $row.'Manager Verification'
            AccountingVerified = ConvertTo-SqlBoolean $row.'Accounting Verification'
            SourceSpreadsheetID = ConvertTo-SqlText $spreadsheetId
            SourceSheet = ConvertTo-SqlText $Sheet
            SourceRow = $index + 2
        }
    }
}

$records = @(
    Get-ExpenseRows -Path $MaintenanceCsv -Sheet 'Maintenance_and_other_expenses' -Maintenance
    if ($SafetyCsv) {
        Get-ExpenseRows -Path $SafetyCsv -Sheet 'Safety Expenses' -FixedCategory 'Safety'
    }
    if ($HRCsv) {
        Get-ExpenseRows -Path $HRCsv -Sheet 'HR expenses' -FixedCategory 'HR'
    }
    if ($AdministrativeCsv) {
        Get-ExpenseRows -Path $AdministrativeCsv -Sheet 'Administrative Expenses' -FixedCategory 'Administrative'
    }
)

$lines = [Collections.Generic.List[string]]::new()
$lines.Add('BEGIN;')
$lines.Add('SET LOCAL statement_timeout = ''10min'';')
if ($ReplaceAllExpenses) {
    $lines.Add('DELETE FROM expenses;')
}
if ($AllowLegacyUnassignedTruckOwnerBefore) {
    $lines.Add('ALTER TABLE expenses DISABLE TRIGGER expenses_00_responsibility;')
}
$columns = @(
    'category', 'category_id', 'expense_date', 'truck_id', 'driver_id', 'owner_id', 'unit_number', 'driver_name', 'amount',
    'payment_type', 'expense_type', 'reference_number', 'description', 'covered_by',
    'paid_by', 'manager_verified', 'accounting_verified', 'source_spreadsheet_id',
    'source_sheet', 'source_row'
) -join ', '

for ($offset = 0; $offset -lt $records.Count; $offset += 250) {
    $end = [Math]::Min($offset + 249, $records.Count - 1)
    $tuples = [Collections.Generic.List[string]]::new()
    for ($index = $offset; $index -le $end; $index++) {
        $row = $records[$index]
        $normalizedUnit = "upper(regexp_replace(btrim($($row.UnitNumber)::text), '\s+', ' ', 'g'))"
        $truckID = "(SELECT id FROM trucks WHERE upper(regexp_replace(btrim(unit_number), '\s+', ' ', 'g')) = $normalizedUnit LIMIT 1)"
        $driverID = "(SELECT id FROM drivers WHERE normalized_name = lower(regexp_replace(btrim($($row.DriverName)::text), '\s+', ' ', 'g')) LIMIT 1)"
        $ownerID = "CASE WHEN lower(btrim($($row.CoveredBy)::text)) = 'truck owner' THEN (SELECT owner_id FROM trucks WHERE upper(regexp_replace(btrim(unit_number), '\s+', ' ', 'g')) = $normalizedUnit LIMIT 1) ELSE NULL END"
        $tuples.Add("($($row.Category), (SELECT id FROM expense_settings WHERE kind = 'category' AND active AND name = $($row.Category)), $($row.ExpenseDate), $truckID, $driverID, $ownerID, $($row.UnitNumber), $($row.DriverName), $($row.Amount), $($row.PaymentType), $($row.ExpenseType), $($row.ReferenceNumber), $($row.Description), $($row.CoveredBy), $($row.PaidBy), $($row.ManagerVerified), $($row.AccountingVerified), $($row.SourceSpreadsheetID), $($row.SourceSheet), $($row.SourceRow))")
    }
    $lines.Add("INSERT INTO expenses ($columns) VALUES")
    $lines.Add(($tuples -join ",`n"))
    $lines.Add(@'
ON CONFLICT (source_spreadsheet_id, source_sheet, source_row)
    WHERE source_spreadsheet_id IS NOT NULL
DO UPDATE SET
    category = EXCLUDED.category,
    category_id = EXCLUDED.category_id,
    expense_date = EXCLUDED.expense_date,
    unit_number = EXCLUDED.unit_number,
    driver_name = EXCLUDED.driver_name,
    amount = EXCLUDED.amount,
    payment_type = EXCLUDED.payment_type,
    expense_type = EXCLUDED.expense_type,
    reference_number = EXCLUDED.reference_number,
    description = EXCLUDED.description,
    covered_by = EXCLUDED.covered_by,
    paid_by = EXCLUDED.paid_by,
    -- Reimports preserve approvals and cancellations made in the application.
    updated_at = now();
'@)
}

$lines.Add(@"
-- Imported historical checks have no recorded verifier or timestamp.
INSERT INTO expense_verifications(expense_id, verifier_role)
SELECT id, 'admin' FROM expenses
WHERE source_spreadsheet_id = '$spreadsheetId' AND manager_verified
ON CONFLICT (expense_id, verifier_role) WHERE revoked_at IS NULL DO NOTHING;

INSERT INTO expense_verifications(expense_id, verifier_role)
SELECT id, 'accountant' FROM expenses
WHERE source_spreadsheet_id = '$spreadsheetId' AND accounting_verified
ON CONFLICT (expense_id, verifier_role) WHERE revoked_at IS NULL DO NOTHING;

UPDATE expenses AS expense
SET truck_id = truck.id
FROM trucks AS truck
WHERE expense.source_spreadsheet_id = '$spreadsheetId'
    AND expense.truck_id IS NULL
    AND expense.unit_number IS NOT NULL
    AND upper(regexp_replace(btrim(expense.unit_number), '\s+', ' ', 'g')) = truck.unit_number;

UPDATE expenses AS expense
SET driver_id = driver.id
FROM drivers AS driver
WHERE expense.source_spreadsheet_id = '$spreadsheetId'
    AND expense.driver_id IS NULL
    AND expense.driver_name IS NOT NULL
    AND lower(regexp_replace(btrim(expense.driver_name), '\s+', ' ', 'g')) = driver.normalized_name;

UPDATE expenses
SET charge_driver_id = driver_id
WHERE source_spreadsheet_id = '$spreadsheetId'
    AND lower(btrim(covered_by)) = 'driver'
    AND driver_id IS NOT NULL;

UPDATE expenses AS expense
SET charge_driver_id = investor.driver_id
FROM investors AS investor
WHERE expense.source_spreadsheet_id = '$spreadsheetId'
    AND lower(btrim(expense.covered_by)) = 'truck owner'
    AND expense.owner_id = investor.id
    AND NOT investor.is_company;
"@)

if ($AllowLegacyUnassignedTruckOwnerBefore) {
    $legacyCutoff = $AllowLegacyUnassignedTruckOwnerBefore.ToString('yyyy-MM-dd', $invariantCulture)
    $lines.Add('ALTER TABLE expenses ENABLE TRIGGER expenses_00_responsibility;')
    $lines.Add(@"
UPDATE expenses AS expense
SET covered_by = 'Company', owner_id = NULL, charge_driver_id = NULL
FROM investors AS investor
WHERE expense.source_spreadsheet_id = '$spreadsheetId'
    AND lower(btrim(expense.covered_by)) = 'truck owner'
    AND expense.owner_id = investor.id
    AND investor.is_company;

DO `$`$
BEGIN
    IF EXISTS (
        SELECT 1 FROM expenses
        WHERE source_spreadsheet_id = '$spreadsheetId'
          AND lower(btrim(covered_by)) = 'truck owner'
          AND owner_id IS NULL
          AND (expense_date IS NULL OR expense_date >= '$legacyCutoff')
    ) THEN
        RAISE EXCEPTION 'unassigned truck-owner expenses must predate $legacyCutoff';
    END IF;
END
`$`$;
"@)
}

if ($SettleBefore) {
    $settleBeforeDate = $SettleBefore.ToString('yyyy-MM-dd', $invariantCulture)
    $lines.Add(@"
UPDATE expenses
SET driver_settled = true
WHERE source_spreadsheet_id = '$spreadsheetId'
    AND expense_date < '$settleBeforeDate';
"@)
}

$lines.Add("DO `$`$")
$lines.Add('DECLARE imported_count integer; incomplete_count integer;')
$lines.Add('BEGIN')
$lines.Add("    SELECT count(*), count(*) FILTER (WHERE expense_date IS NULL OR amount IS NULL)")
$lines.Add("    INTO imported_count, incomplete_count FROM expenses WHERE source_spreadsheet_id = '$spreadsheetId';")
$lines.Add("    IF imported_count <> $($records.Count) THEN")
$lines.Add("        RAISE EXCEPTION 'expected $($records.Count) imported expenses, found %', imported_count;")
$lines.Add('    END IF;')
$lines.Add("    RAISE NOTICE 'verified % imported expenses (% incomplete)', imported_count, incomplete_count;")
$lines.Add('END')
$lines.Add("`$`$;")
$lines.Add('COMMIT;')

[IO.File]::WriteAllLines(
    [IO.Path]::GetFullPath($OutputSql),
    $lines,
    [Text.UTF8Encoding]::new($false)
)

[pscustomobject]@{
    OutputSql = [IO.Path]::GetFullPath($OutputSql)
    ExpenseCount = $records.Count
    MissingDate = @($records | Where-Object ExpenseDate -eq 'NULL').Count
    MissingAmount = @($records | Where-Object Amount -eq 'NULL').Count
}
