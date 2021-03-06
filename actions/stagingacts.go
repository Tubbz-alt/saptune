package actions

import (
	"fmt"
	"github.com/SUSE/saptune/app"
	"github.com/SUSE/saptune/sap/note"
	"github.com/SUSE/saptune/sap/solution"
	"github.com/SUSE/saptune/system"
	"github.com/SUSE/saptune/txtparser"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"strconv"
	"strings"
)

type stageFiles struct {
	AllStageFiles   []string
	StageAttributes map[string]map[string]string
}

type stageComparison struct {
	FieldName        string
	stgVal, wrkVal   string
	MatchExpectation bool
}

var saptuneSysconfig = "/etc/sysconfig/saptune"
var stagingSwitch = false
var stagingOptions = note.GetTuningOptions(StagingSheets, "")
var stgFiles stageFiles

// StagingAction  Staging actions like apply, revert, verify asm.
func StagingAction(actionName string, stageName []string, tuneApp *app.App) {
	stagingSwitch = getStagingFromConf()
	if len(stgFiles.AllStageFiles) == 0 && len(stgFiles.StageAttributes) == 0 {
		stgFiles = collectStageFileInfo(tuneApp)
	}

	switch actionName {
	case "status":
		stagingActionStatus(os.Stdout)
	case "is-enabled":
		// Returns the status of staging as exit code
		// 0 == enabled (STAGING=true), 1 == disabled (STAGING=false)
		if stagingSwitch {
			system.ErrorExit("", 0)
		} else {
			system.ErrorExit("", 1)
		}
	case "enable":
		stagingActionEnable()
	case "disable":
		stagingActionDisable()
	case "list":
		chkStageExit(os.Stdout)
		stagingActionList(os.Stdout)
	case "diff":
		if len(stageName) == 0 {
			stageName = []string{"all"}
		}
		chkStageExit(os.Stdout)
		stagingActionDiff(os.Stdout, stageName)
	case "analysis":
		if len(stageName) == 0 {
			stageName = []string{"all"}
		}
		chkStageExit(os.Stdout)
		stagingActionAnalysis(os.Stdout, stageName)
	case "release":
		if len(stageName) == 0 {
			stageName = []string{"all"}
		}
		chkStageExit(os.Stdout)
		stagingActionRelease(os.Stdin, os.Stdout, stageName)
	default:
		PrintHelpAndExit(os.Stdout, 1)
	}
}

// stagingActionStatus shows the status of staging,
// basically the content of STAGING in /etc/sysconfig/saptune.
func stagingActionStatus(writer io.Writer) {
	if stagingSwitch {
		system.InfoLog("STAGING variable is 'true'")
		fmt.Fprintf(writer, "Staging is enabled\n")
	} else {
		system.InfoLog("STAGING variable is 'false'")
		fmt.Fprintf(writer, "Staging is disabled\n")
	}
}

// stagingActionEnable enables staging by setting STAGING in /etc/sysconfig/saptune.
func stagingActionEnable() {
	system.InfoLog("Enable staging")
	stagingSwitch = true
	if err := writeStagingToConf("true"); err != nil {
		system.ErrorExit("Staging could NOT be enabled. - '%v'\n", err, 122)
	}
	system.InfoLog("Staging has been enabled.")
}

// stagingActionDisable disables staging by setting STAGING in /etc/sysconfig/saptune.
func stagingActionDisable() {
	system.InfoLog("Disable staging")
	stagingSwitch = false
	if err := writeStagingToConf("false"); err != nil {
		system.ErrorExit("Staging could NOT be disabled. - '%v'\n", err, 123)
	}
	system.InfoLog("Staging has been disabled.")
}

// stagingActionList lists all Notes and solution definition which can be
// released from the staging area.
// If a Note or the solution definition is part of the working area, but not
// in the package area, it will be listed as deleted.
func stagingActionList(writer io.Writer) {
	fmt.Fprintf(writer, "\n")
	for _, stageName := range stgFiles.AllStageFiles {
		desc := stgFiles.StageAttributes[stageName]["desc"]
		flag := ""
		flags := []string{"deleted", "updated", "new"}
		for _, f := range flags {
			if stgFiles.StageAttributes[stageName][f] == "true" {
				flag = fmt.Sprintf("(%s)", f)
				break
			}
		}
		format := "\t%s\t\t%s\n\t\t\t%s\n"
		if len(stageName) >= 8 {
			format = "\t%s\t%s\n\t\t\t%s\n"
		}
		fmt.Fprintf(writer, format, stageName, desc, flag)
	}
	fmt.Fprintf(writer, "\nRemember: To release from staging use the command 'saptune staging release ...'.\n          You can check the differences with 'saptune staging diff ...'.\n")
}

// stagingActionDiff shows the differences between the Note, the solution definition
// or all objects in the staging area and the working area.
// For each Note in the staging area the output contains the values of all
// parameter which differ.
// This includes new or removed parameters as well as changes in the reminder
// section.
// For the Solution, all changed solutions are displayed with their differences.
func stagingActionDiff(writer io.Writer, sObject []string) {
	for _, sName := range sObject {
		switch sName {
		case "all":
			for _, stageName := range stgFiles.AllStageFiles {
				diffStageObj(writer, stageName)
			}
		default:
			diffStageObj(writer, sName)
		}
	}
	fmt.Fprintf(writer, "\nRemember: To release from staging use the command 'saptune staging release ...'.\n")
}

// stagingActionAnalysis does an analysis of the requested Notes, the solution
// definition or everything in the staging area to warn the user about possible
// issues or additional steps to perform.
func stagingActionAnalysis(writer io.Writer, stageObject []string) {
	fmt.Fprintf(writer, "\n")
	for _, sObj := range stageObject {
		switch sObj {
		case "all":
			for _, stageName := range stgFiles.AllStageFiles {
				showAnalysis(writer, stageName)
			}
		default:
			showAnalysis(writer, sObj)
		}
	}
	fmt.Fprintf(writer, "\nRemember: To release from staging use the command 'saptune staging release ...'. Check the differences first with 'saptune staging diff...'.\n")
}

// StagingActionRelease releases the requested Notes, the solution definition or
// everything in the stages area.
// This means the Notes or the solution definition gets moved from the staging
// area to the working area.
// In case of a deleted Note, it will be removed from the working area.
// First the command will show an analysis of the objects going to be released
// to make the user aware of further needed actions or potential problems
// (for details see saptune staging analysis).
// The customer has to confirm this, because the action is irreversible.
func stagingActionRelease(reader io.Reader, writer io.Writer, sObject []string) {
	for _, sName := range sObject {
		stagingFile := stgFiles.StageAttributes[sName]["sfilename"]
		stageVers := stgFiles.StageAttributes[sName]["version"]
		stageDate := stgFiles.StageAttributes[sName]["date"]

		switch sName {
		case "all":
			for _, stageName := range stgFiles.AllStageFiles {
				showAnalysis(writer, stageName)
			}
			// ANGI TODO - parse command line to set ForceFlag or DryRunFlag or other Flags
			//if DryRunFlag {
			//	system.ErrorExit("", 0)
			//}
			// ANGI TODO - parse command line to set ForceFlag or DryRunFlag or other Flags
			// if !ForceFlag {
			txtConfirm := fmt.Sprintf("Releasing is irreversible! Are you sure")
			if !readYesNo(txtConfirm, reader, writer) {
				system.ErrorExit("", 0)
			}
			//}
			errs := make([]error, 0, 0)
			for _, stageName := range stgFiles.AllStageFiles {
				stagingFile = stgFiles.StageAttributes[stageName]["sfilename"]
				if _, err := os.Stat(stagingFile); err != nil {
					system.ErrorLog("file '%s' not found in staging area, nothing to do, skipping ...", stagingFile)
					errs = append(errs, err)
				}
				stageVers = stgFiles.StageAttributes[stageName]["version"]
				stageDate = stgFiles.StageAttributes[stageName]["date"]
				err := mvStageToWork(stageName)
				if err != nil {
					errs = append(errs, err)
				} else {
					system.InfoLog("%s Version %s (%s) released", stageName, stageVers, stageDate)
				}
			}
			if len(errs) != 0 {
				system.ErrorExit("", 126)
			}
		default:
			if stagingFile == "" {
				system.ErrorExit("'%s' not found in staging area, nothing to do.", sName, 127)
			}
			showAnalysis(writer, sName)
			txtConfirm := fmt.Sprintf("Releasing is irreversible! Are you sure")
			if !readYesNo(txtConfirm, reader, writer) {
				system.ErrorExit("", 0)
			}
			if err := mvStageToWork(sName); err != nil {
				system.ErrorExit("", 128)
			}
			system.InfoLog("%s Version %s (%s) released", sName, stageVers, stageDate)
		}
	}
}

// showAnalysis does an analysis of the requested object in the staging area
// to warn the user about possible issues or additional steps to perform.
func showAnalysis(writer io.Writer, stageName string) {
	if stageName == "" {
		PrintHelpAndExit(writer, 0)
	}

	txtPrefix := "    --> "
	txtReleaseNote := "Release of %s Version %s (%s)\n"
	vers := stgFiles.StageAttributes[stageName]["version"]
	date := stgFiles.StageAttributes[stageName]["date"]
	flag := ""
	flags := []string{"deleted", "updated", "new"}
	for _, f := range flags {
		if stgFiles.StageAttributes[stageName][f] == "true" {
			flag = f
			break
		}
	}
	if flag != "deleted" {
		fmt.Fprintf(writer, txtReleaseNote, stageName, vers, date)
	}
	if stageName == "solutions" {
		stgSols := solution.GetSolutionDefintion(stgFiles.StageAttributes[stageName]["sfilename"])
		stageSols, exist := stgSols[system.GetSolutionSelector()]
		if !exist {
			system.ErrorExit("No solution definition available for system architecture '%s'.", system.GetSolutionSelector())
			return
		}
		// print solution analysis
		printSolAnalysis(writer, stageName, txtPrefix, stageSols)
	} else {
		// print note analysis
		printNoteAnalysis(writer, stageName, txtPrefix, flag)
	}
}

// printSolAnalysis handles the solution related analysis
func printSolAnalysis(writer io.Writer, stageName, txtPrefix string, stageSols map[string]solution.Solution) {
	txtSolEnabled := txtPrefix + "Solution '%s' is enabled and must be re-applied.\n"
	txtRequiredNote := txtPrefix + "Solution '%s' requires releasing of '%s' or it breaks!\n"
	txtDelSolEnabled := txtPrefix + "Solution '%s' is currently enabled, but now deleted. Must be reverted.\n"
	sols := []string{}
	for sol := range stageSols {
		sols = append(sols, sol)
	}
	sort.Strings(sols)
	escnt := 0
	for _, sol := range sols {
		if sol == stgFiles.StageAttributes[stageName]["enabledSol"] {
			fmt.Fprintf(writer, txtSolEnabled, sol)
			escnt = escnt + 1
		}
		for _, noteID := range stageSols[sol] {
			for _, stgName := range stgFiles.AllStageFiles {
				if stgName == noteID {
					fmt.Fprintf(writer, txtRequiredNote, sol, stgName)
				}
			}
		}
	}
	if stgFiles.StageAttributes[stageName]["enabledSol"] != "" && escnt == 0 {
		fmt.Fprintf(writer, txtDelSolEnabled, stgFiles.StageAttributes[stageName]["enabledSol"])
	}
}

// printNoteAnalysis handles the solution related analysis
func printNoteAnalysis(writer io.Writer, stageName, txtPrefix, flag string) {
	txtDeleteNote := "Deletion of %s\n"
	txtOverrideExists := txtPrefix + "Override file exists and might need adjustments.\n"
	txtNoteEnabled := txtPrefix + "Note is enabled and must be reapplied.\n"
	txtNoteNotEnabled := txtPrefix + "Note is not enabled, no action required.\n"
	txtSolEnabled := txtPrefix + "Note is part of the currently enabled solution '%s'.\n"
	txtSolNotEnabled := txtPrefix + "Note is part of the not-enabled solution(s) '%s'\n"
	//txtCustomSolEnabled := txtPrefix + "Note is part of the currently enabled custom solution '%s'.\n"
	//txtCustomSolNotEnabled := txtPrefix + "Note is part of the not-enabled custom solution(s) '%s'.\n"

	if flag == "deleted" {
		txtOverrideExists = txtPrefix + "Override file exists and can be deleted.\n"
		txtNoteEnabled = txtPrefix + "Note is enabled and must be reverted.\n"
		txtSolEnabled = txtPrefix + "Note is part of the currently enabled solution '%s'. Release would break the solution!\n"
		txtSolNotEnabled = txtPrefix + "Note is part of the not-enabled solution(s) '%s'. Release would break the solution(s)!\n"
		//txtCustomSolEnabled = txtPrefix + "Note is part of the currently enabled custom solution '%s'. Release would break the solution!\n"
		//txtCustomSolNotEnabled = txtPrefix + "Note is part of the not-enabled custom solution(s) '%s'. Release would break the solution!\n"

		fmt.Fprintf(writer, txtDeleteNote, stageName)
	}
	if stgFiles.StageAttributes[stageName]["override"] == "true" {
		fmt.Fprintf(writer, txtOverrideExists)
	}
	if flag != "new" {
		// ANGI TODO - ask Soeren, if enabled (only in the variable) or applied (saved_state file available)
		//if stgFiles.StageAttributes[stageName]["enabled"] == "true" {
		if stgFiles.StageAttributes[stageName]["applied"] == "true" {
			fmt.Fprintf(writer, txtNoteEnabled)
		} else {
			fmt.Fprintf(writer, txtNoteNotEnabled)
		}
	}
	if stgFiles.StageAttributes[stageName]["inSolution"] != "" {
		for _, sol := range strings.Split(stgFiles.StageAttributes[stageName]["inSolution"], ",") {
			sol = strings.TrimSpace(sol)
			if sol == stgFiles.StageAttributes[stageName]["enabledSol"] {
				fmt.Fprintf(writer, txtSolEnabled, sol)
			} else {
				fmt.Fprintf(writer, txtSolNotEnabled, sol)
			}
		}
	}
}

// mvStageToWork moves a file from the staging area to the working area
// or removes deleted files from the working area
func mvStageToWork(stageName string) error {
	stagingFile := stgFiles.StageAttributes[stageName]["sfilename"]
	workingFile := stgFiles.StageAttributes[stageName]["wfilename"]
	packageFile := stgFiles.StageAttributes[stageName]["pfilename"]
	// check, if note should be deleted
	if _, err := os.Stat(workingFile); err == nil {
		if _, perr := os.Stat(packageFile); os.IsNotExist(perr) {
			// in working, but not in packaging, delete from working and staging
			errs := make([]error, 0, 0)
			if rerr := os.Remove(workingFile); rerr != nil {
				system.ErrorLog("Problems during removal of '%s' from working area: %v", stageName, rerr)
				errs = append(errs, rerr)
			}
			if rerr := os.Remove(stagingFile); rerr != nil {
				system.ErrorLog("Problems during removal of '%s' from staging area: %v", stageName, rerr)
				errs = append(errs, rerr)
			}
			if len(errs) != 0 {
				return fmt.Errorf("Problems during removal of deleted Note '%s'", stageName)
			}
			return nil
		}
	}
	// move new or changed/updated note/solution from staging to working area
	if err := os.Rename(stagingFile, workingFile); err != nil {
		system.ErrorLog("Problems during move of '%s' from staging to working area: %v", stageName, err)
		return err
	}
	return nil
}

// getStagingFromConf reads STAGING setting from /etc/sysconfig/saptune
func getStagingFromConf() bool {
	sconf, err := txtparser.ParseSysconfigFile(saptuneSysconfig, true)
	if err != nil {
		system.ErrorExit("Unable to read file '/etc/sysconfig/saptune': '%v'\n", err, 1)
	}
	if sconf.GetString("STAGING", "false") == "true" {
		stagingSwitch = true
	}
	return stagingSwitch
}

// writeStagingToConf writes STAGING setting to /etc/sysconfig/saptune
func writeStagingToConf(staging string) error {
	sconf, err := txtparser.ParseSysconfigFile(saptuneSysconfig, true)
	if err != nil {
		return err
	}
	sconf.Set("STAGING", staging)
	return ioutil.WriteFile(saptuneSysconfig, []byte(sconf.ToText()), 0644)
}

//func collectStageFileInfo() *stageFiles {
func collectStageFileInfo(tuneApp *app.App) stageFiles {
	stageConf := stageFiles{
		AllStageFiles:   make([]string, 0, 64),
		StageAttributes: make(map[string]map[string]string),
	}
	stageMap := make(map[string]string)

	for _, stageName := range stagingOptions.GetSortedIDs() {
		// add new stage file
		stageMap = make(map[string]string)

		// get Note Description and setup absolute filenames
		noteObj := stagingOptions[stageName]
		name := noteObj.Name()

		stagingFile := fmt.Sprintf("%s/%s", StagingSheets, stageName)
		workingFile := fmt.Sprintf("%snotes/%s", WorkingArea, stageName)
		packageFile := fmt.Sprintf("%snotes/%s", PackageArea, stageName)
		if stageName == "solutions" {
			if name == "" {
				name = fmt.Sprintf("Definition of saptune solutions\n\t\t\tVersion 1")
			}
			workingFile = fmt.Sprintf("%s%s", WorkingArea, stageName)
			packageFile = fmt.Sprintf("%s%s", PackageArea, stageName)
		}

		// Description
		stageMap["desc"] = name
		// Version
		stageMap["version"] = txtparser.GetINIFileVersionSectionEntry(stagingFile, "version")
		// Date
		stageMap["date"] = txtparser.GetINIFileVersionSectionEntry(stagingFile, "date")
		// filenames
		stageMap["wfilename"] = workingFile
		stageMap["pfilename"] = packageFile
		stageMap["sfilename"] = stagingFile
		// enabled solution
		if len(tuneApp.TuneForSolutions) > 0 {
			stageMap["enabledSol"] = tuneApp.TuneForSolutions[0]
		}

		// get flags
		stageMap["new"] = "false"
		stageMap["deleted"] = "false"
		stageMap["updated"] = "true"

		if _, err := os.Stat(workingFile); os.IsNotExist(err) {
			// not in working, but in staging
			// new Note
			stageMap["new"] = "true"
			stageMap["updated"] = "false"
		} else if err == nil {
			if _, perr := os.Stat(packageFile); os.IsNotExist(perr) {
				// in working, but not in packaging
				// deleted Note
				stageMap["deleted"] = "true"
				stageMap["updated"] = "false"
			}
		}
		// check for override file
		stageMap["override"] = "false"
		if _, override := getovFile(stageName, OverrideTuningSheets); override {
			stageMap["override"] = "true"
		}
		// check if applied
		stageMap["applied"] = "false"
		if _, ok := tuneApp.IsNoteApplied(stageName); ok {
			stageMap["applied"] = "true"
		}
		// check if enabled
		stageMap["enabled"] = "true"
		if tuneApp.PositionInNoteApplyOrder(stageName) < 0 { // noteID not yet available
			stageMap["enabled"] = "false"
		}

		// check if in a solution
		noteInSols := ""
		sols := []string{}
		for sol := range tuneApp.AllSolutions {
			sols = append(sols, sol)
		}
		sort.Strings(sols)
		for _, sol := range sols {
			for _, noteID := range tuneApp.AllSolutions[sol] {
				if stageName == noteID {
					// stageName is part of solution sol
					if len(noteInSols) == 0 {
						noteInSols = sol
					} else {
						noteInSols = fmt.Sprintf("%s, %s", noteInSols, sol)
					}
				}
			}
		}
		stageMap["inSolution"] = noteInSols
		// ANGI TODO - check for custom solution

		stageConf.StageAttributes[stageName] = stageMap
		stageConf.AllStageFiles = append(stageConf.AllStageFiles, stageName)
	}
	return stageConf
}

// diffStageObj diffs a note from the staging area with a note from the working area
func diffStageObj(writer io.Writer, sName string) {
	var workingNote *txtparser.INIFile
	stgNote := map[string]string{}
	wrkNote := map[string]string{}
	solSelect := "ArchX86"
	if system.GetSolutionSelector() == "ppc64le" {
		solSelect = "ArchPPC64LE"
	}
	// parse staging file
	stagingNote, err := txtparser.ParseINIFile(stgFiles.StageAttributes[sName]["sfilename"], false)
	if err != nil {
		system.ErrorLog("Problems while parsing the staging Note definition file. Check the name")
		return
	}
	for _, param := range stagingNote.AllValues {
		if sName == "solutions" && param.Section != solSelect {
			continue
		}
		stgNote[param.Key] = param.Value
	}

	if stgFiles.StageAttributes[sName]["new"] != "true" {
		// parse working file
		workingNote, err = txtparser.ParseINIFile(stgFiles.StageAttributes[sName]["wfilename"], false)
		if err != nil {
			system.ErrorLog("Problems while parsing the working Note definition file. Check the name")
			return
		}
		for _, param := range workingNote.AllValues {
			if sName == "solutions" && param.Section != solSelect {
				continue
			}
			wrkNote[param.Key] = param.Value
		}
	}

	conforming, comparisons := compareStageFields(sName, stgNote, wrkNote)
	if !conforming {
		PrintStageFields(writer, sName, comparisons)
	} else {
		// paranoia log, should not be the case, because the saptune rpm takes care of this
		system.InfoLog("no diffs in staging")
	}
}

// compareStageFields compares a note from the staging area with a note from the working area
func compareStageFields(sName string, stage, work map[string]string) (allMatch bool, comparisons map[string]stageComparison) {
	comparisons = make(map[string]stageComparison)
	allMatch = true
	// check for deleted Notes
	if stgFiles.StageAttributes[sName]["deleted"] == "true" {
		for Key, workValue := range work {
			comparisons[Key] = stageComparison{
				FieldName:        Key,
				stgVal:           "-",
				wrkVal:           workValue,
				MatchExpectation: false,
			}
			allMatch = false
		}
		return
	}
	// check for new Notes
	if stgFiles.StageAttributes[sName]["new"] == "true" {
		for Key, stageValue := range stage {
			comparisons[Key] = stageComparison{
				FieldName:        Key,
				stgVal:           stageValue,
				wrkVal:           "-",
				MatchExpectation: false,
			}
			allMatch = false
		}
		return
	}

	// changed Notes
	// check for deleted parameter in staging Note
	for Key, workValue := range work {
		if stage[Key] != "" {
			continue
		}
		comparisons[Key] = stageComparison{
			FieldName:        Key,
			stgVal:           "-",
			wrkVal:           workValue,
			MatchExpectation: false,
		}
		allMatch = false
	}
	for Key, stageValue := range stage {
		// new/additional parameter settings in staging Note - workValue will be '' and match is false
		// no extra handling needed
		workValue := work[Key]
		stageValueJS, workValueJS, match := note.CompareJSValue(stageValue, workValue, "")
		if !match {
			if workValueJS == "" {
				workValueJS = "-"
			}
			comparisons[Key] = stageComparison{
				FieldName:        Key,
				stgVal:           stageValueJS,
				wrkVal:           workValueJS,
				MatchExpectation: match,
			}
			allMatch = match
		}
	}
	return
}

// PrintStageFields prints mismatching parameters between Notes in staging
// and working area
func PrintStageFields(writer io.Writer, stageName string, comparison map[string]stageComparison) {

	workFile := stgFiles.StageAttributes[stageName]["wfilename"]
	headWork := fmt.Sprintf("Version %s (%s) ", txtparser.GetINIFileVersionSectionEntry(workFile, "version"), txtparser.GetINIFileVersionSectionEntry(workFile, "date"))
	headStage := fmt.Sprintf("Version %s (%s) ", stgFiles.StageAttributes[stageName]["version"], stgFiles.StageAttributes[stageName]["date"])

	// sort output
	sortkeys := sortStageComparisonsOutput(comparison)

	// setup table format values
	fmtdash, fmtplus, format := setupStageTableFormat(comparison)

	// print table header
	fmt.Fprintf(writer, fmtdash)
	fmt.Fprintf(writer, format, stageName, headWork, headStage)
	fmt.Fprintf(writer, format, "", "(working area)", "(staging area)")
	fmt.Fprintf(writer, fmtplus)
	for _, skey := range sortkeys {
		// print table body
		if skey == "reminder" {
			fmt.Fprintf(writer, format, skey, "diff need to be done", "")
		} else {
			fmt.Fprintf(writer, format, skey, strings.Replace(comparison[skey].wrkVal, "\t", " ", -1), strings.Replace(comparison[skey].stgVal, "\t", " ", -1))
		}
	}
	// print footer
	fmt.Fprintf(writer, fmtdash)
	fmt.Fprintf(writer, "\n")
}

// sortStageComparisonsOutput sorts the output of the stage comparison
// the reminder section should be the last one
func sortStageComparisonsOutput(noteCompare map[string]stageComparison) []string {
	skeys := make([]string, 0, len(noteCompare))
	rkeys := make([]string, 0, len(noteCompare))
	// sort output
	for key := range noteCompare {
		if key != "reminder" {
			skeys = append(skeys, key)
		} else {
			rkeys = append(rkeys, key)
		}
	}
	sort.Strings(skeys)
	for _, rem := range rkeys {
		skeys = append(skeys, rem)
	}
	return skeys
}

// setupStageTableFormat sets the format of the table columns dependent on the content
func setupStageTableFormat(stageCompare map[string]stageComparison) (string, string, string) {
	var fmtdash string
	var fmtplus string
	var format string
	// define start values for the column width
	fmtlen1 := 12
	fmtlen2 := 26
	fmtlen3 := 26

	for skey, comparison := range stageCompare {
		// ANGI TODO - reminder handling (split lines so that they fit the column width, more than one line for this parameter possible
		// 1:parameter, 2:working, 3:staging
		if len(skey) > fmtlen1 {
			fmtlen1 = len(skey)
		}
		if len(comparison.wrkVal) > fmtlen2 {
			fmtlen2 = len(comparison.wrkVal)
		}
		if len(comparison.stgVal) > fmtlen3 {
			fmtlen3 = len(comparison.stgVal)
		}
	}

	format = " %-" + strconv.Itoa(fmtlen1) + "s | %-" + strconv.Itoa(fmtlen2) + "s | %-" + strconv.Itoa(fmtlen3) + "s \n"

	tableLen := fmtlen1 + fmtlen2 + fmtlen3 + 8
	// line with dashes, used as borders for the table
	for i := 0; i < tableLen; i++ {
		fmtdash = fmtdash + "-"
	}
	fmtdash = fmtdash + "\n"
	// line with dashes and plus, used as separator between head and body
	for i := 0; i < tableLen; i++ {
		if i == 1+fmtlen1+1 || i == 1+fmtlen1+3+fmtlen2+1 || i == 1+fmtlen1+3+fmtlen2+4+fmtlen3+1 {
			fmtplus = fmtplus + "+"
		} else {
			fmtplus = fmtplus + "-"
		}
	}
	fmtplus = fmtplus + "\n"
	return fmtdash, fmtplus, format
}

// chkStageExit checks, if a staging action should be executed or not
func chkStageExit(writer io.Writer) {
	if !stagingSwitch {
		fmt.Fprintf(writer, "ATTENTION: Staging is currently disabled. Please enable staging first and try again.\n")
		system.ErrorExit("", 0)
	}
	if len(stagingOptions.GetSortedIDs()) == 0 {
		fmt.Fprintf(writer, "Empty staging area, no Notes or solutions available. So nothing to do\n")
		system.ErrorExit("", 0)
	}
}
