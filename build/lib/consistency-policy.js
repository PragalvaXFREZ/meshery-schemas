"use strict";

function classifyStyleIssue({ strictConsistency, strictStyleFile, includeLegacyStyleDebt }) {
  if (strictConsistency || strictStyleFile) {
    return "error";
  }

  if (includeLegacyStyleDebt) {
    return "warning";
  }

  return null;
}

function classifyDesignIssue({ strictConsistency }) {
  return strictConsistency ? "error" : "warning";
}

function classifyContractIssue({ strictConsistency, strictStyleFile, includeLegacyContractDebt }) {
  if (strictConsistency || strictStyleFile) {
    return "error";
  }

  if (includeLegacyContractDebt) {
    return "warning";
  }

  return null;
}

module.exports = {
  classifyContractIssue,
  classifyDesignIssue,
  classifyStyleIssue,
};