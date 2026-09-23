param(
    [Parameter(Mandatory = $true)]
    [string]$MaintenanceCsv,
    [Parameter(Mandatory = $true)]
    [string]$SafetyCsv,
    [Parameter(Mandatory = $true)]
    [string]$HRCsv,
    [Parameter(Mandatory = $true)]
    [string]$AdministrativeCsv,
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
        if (-not (
            $row.Date -or $row.Amount -or $row.Description -or
            $row.'Expense Type' -or $row.Driver -or $row.'Unit #'
        )) {
            continue
        }

        $company = if ($Maintenance) { $row.H1 } else { $row.Company }
        $category = if ($FixedCategory) { $FixedCategory } else { ([string]$row.Category).Trim() }
        if ($category.Length -eq 0) { $category = 'Other' }

        [pscustomobject]@{
            Company = ConvertTo-SqlText $company -PreserveEmpty
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
    Get-ExpenseRows -Path $SafetyCsv -Sheet 'Safety Expenses' -FixedCategory 'Safety'
    Get-ExpenseRows -Path $HRCsv -Sheet 'HR expenses' -FixedCategory 'HR'
    Get-ExpenseRows -Path $AdministrativeCsv -Sheet 'Administrative Expenses' -FixedCategory 'Administrative'
)

$lines = [Collections.Generic.List[string]]::new()
$lines.Add('BEGIN;')
$lines.Add('SET LOCAL statement_timeout = ''10min'';')
$columns = @(
    'company', 'category', 'expense_date', 'unit_number', 'driver_name', 'amount',
    'payment_type', 'expense_type', 'reference_number', 'description', 'covered_by',
    'paid_by', 'manager_verified', 'accounting_verified', 'source_spreadsheet_id',
    'source_sheet', 'source_row'
) -join ', '

for ($offset = 0; $offset -lt $records.Count; $offset += 250) {
    $end = [Math]::Min($offset + 249, $records.Count - 1)
    $tuples = [Collections.Generic.List[string]]::new()
    for ($index = $offset; $index -le $end; $index++) {
        $row = $records[$index]
        $tuples.Add("($($row.Company), $($row.Category), $($row.ExpenseDate), $($row.UnitNumber), $($row.DriverName), $($row.Amount), $($row.PaymentType), $($row.ExpenseType), $($row.ReferenceNumber), $($row.Description), $($row.CoveredBy), $($row.PaidBy), $($row.ManagerVerified), $($row.AccountingVerified), $($row.SourceSpreadsheetID), $($row.SourceSheet), $($row.SourceRow))")
    }
    $lines.Add("INSERT INTO expenses ($columns) VALUES")
    $lines.Add(($tuples -join ",`n"))
    $lines.Add(@'
ON CONFLICT (source_spreadsheet_id, source_sheet, source_row)
    WHERE source_spreadsheet_id IS NOT NULL
DO UPDATE SET
    company = EXCLUDED.company,
    category = EXCLUDED.category,
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
    manager_verified = EXCLUDED.manager_verified,
    accounting_verified = EXCLUDED.accounting_verified,
    updated_at = now();
'@)
}

$lines.Add(@"
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
"@)

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
