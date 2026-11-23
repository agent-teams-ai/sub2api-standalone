// Generated from pinned MIT-licensed typescript-eslint sources; see ../vendor/LICENSE and provenance.json.
var __create = Object.create;
var __defProp = Object.defineProperty;
var __getOwnPropDesc = Object.getOwnPropertyDescriptor;
var __getOwnPropNames = Object.getOwnPropertyNames;
var __getProtoOf = Object.getPrototypeOf;
var __hasOwnProp = Object.prototype.hasOwnProperty;
var __export = (target, all) => {
  for (var name in all)
    __defProp(target, name, { get: all[name], enumerable: true });
};
var __copyProps = (to, from, except, desc) => {
  if (from && typeof from === "object" || typeof from === "function") {
    for (let key of __getOwnPropNames(from))
      if (!__hasOwnProp.call(to, key) && key !== except)
        __defProp(to, key, { get: () => from[key], enumerable: !(desc = __getOwnPropDesc(from, key)) || desc.enumerable });
  }
  return to;
};
var __toESM = (mod, isNodeMode, target) => (target = mod != null ? __create(__getProtoOf(mod)) : {}, __copyProps(
  // If the importer is in node compatibility mode or this is not an ESM
  // file that has been converted to a CommonJS file using a Babel-
  // compatible transform (i.e. "__esModule" has not been set), then set
  // "default" to the CommonJS "module.exports" for node compatibility.
  isNodeMode || !mod || !mod.__esModule ? __defProp(target, "default", { value: mod, enumerable: true }) : target,
  mod
));
var __toCommonJS = (mod) => __copyProps(__defProp({}, "__esModule", { value: true }), mod);

// generated-rule-entry.ts
var generated_rule_entry_exports = {};
__export(generated_rule_entry_exports, {
  default: () => generated_rule_entry_default,
  rules: () => rules
});
module.exports = __toCommonJS(generated_rule_entry_exports);

// vendor/packages/utils/src/ast-utils/index.ts
var ast_utils_exports = {};
__export(ast_utils_exports, {
  LINEBREAK_MATCHER: () => LINEBREAK_MATCHER,
  PatternMatcher: () => PatternMatcher2,
  ReferenceTracker: () => ReferenceTracker2,
  findVariable: () => findVariable2,
  getFunctionHeadLocation: () => getFunctionHeadLocation2,
  getFunctionNameWithKind: () => getFunctionNameWithKind2,
  getInnermostScope: () => getInnermostScope2,
  getPropertyName: () => getPropertyName2,
  getStaticValue: () => getStaticValue2,
  getStringIfConstant: () => getStringIfConstant2,
  hasSideEffect: () => hasSideEffect2,
  isArrowToken: () => isArrowToken2,
  isAwaitExpression: () => isAwaitExpression,
  isAwaitKeyword: () => isAwaitKeyword,
  isClassOrTypeElement: () => isClassOrTypeElement,
  isClosingBraceToken: () => isClosingBraceToken2,
  isClosingBracketToken: () => isClosingBracketToken2,
  isClosingParenToken: () => isClosingParenToken2,
  isColonToken: () => isColonToken2,
  isCommaToken: () => isCommaToken2,
  isCommentToken: () => isCommentToken2,
  isConstructor: () => isConstructor,
  isFunction: () => isFunction,
  isFunctionOrFunctionType: () => isFunctionOrFunctionType,
  isFunctionType: () => isFunctionType,
  isIdentifier: () => isIdentifier,
  isImportKeyword: () => isImportKeyword,
  isLogicalOrOperator: () => isLogicalOrOperator,
  isLoop: () => isLoop,
  isNodeOfType: () => isNodeOfType,
  isNodeOfTypeWithConditions: () => isNodeOfTypeWithConditions,
  isNodeOfTypes: () => isNodeOfTypes,
  isNonNullAssertionPunctuator: () => isNonNullAssertionPunctuator,
  isNotArrowToken: () => isNotArrowToken2,
  isNotClosingBraceToken: () => isNotClosingBraceToken2,
  isNotClosingBracketToken: () => isNotClosingBracketToken2,
  isNotClosingParenToken: () => isNotClosingParenToken2,
  isNotColonToken: () => isNotColonToken2,
  isNotCommaToken: () => isNotCommaToken2,
  isNotCommentToken: () => isNotCommentToken2,
  isNotNonNullAssertionPunctuator: () => isNotNonNullAssertionPunctuator,
  isNotOpeningBraceToken: () => isNotOpeningBraceToken2,
  isNotOpeningBracketToken: () => isNotOpeningBracketToken2,
  isNotOpeningParenToken: () => isNotOpeningParenToken2,
  isNotOptionalChainPunctuator: () => isNotOptionalChainPunctuator,
  isNotSemicolonToken: () => isNotSemicolonToken2,
  isNotTokenOfTypeWithConditions: () => isNotTokenOfTypeWithConditions,
  isOpeningBraceToken: () => isOpeningBraceToken2,
  isOpeningBracketToken: () => isOpeningBracketToken2,
  isOpeningParenToken: () => isOpeningParenToken2,
  isOptionalCallExpression: () => isOptionalCallExpression,
  isOptionalChainPunctuator: () => isOptionalChainPunctuator,
  isParenthesized: () => isParenthesized2,
  isSemicolonToken: () => isSemicolonToken2,
  isSetter: () => isSetter,
  isTSConstructorType: () => isTSConstructorType,
  isTSFunctionType: () => isTSFunctionType,
  isTokenOfTypeWithConditions: () => isTokenOfTypeWithConditions,
  isTokenOnSameLine: () => isTokenOnSameLine,
  isTypeAssertion: () => isTypeAssertion,
  isTypeKeyword: () => isTypeKeyword,
  isVariableDeclarator: () => isVariableDeclarator
});

// vendor/packages/utils/src/ast-utils/eslint-utils/astUtilities.ts
var eslintUtils = __toESM(require("@eslint-community/eslint-utils"), 1);
var getFunctionHeadLocation2 = eslintUtils.getFunctionHeadLocation;
var getFunctionNameWithKind2 = eslintUtils.getFunctionNameWithKind;
var getPropertyName2 = eslintUtils.getPropertyName;
var getStaticValue2 = eslintUtils.getStaticValue;
var getStringIfConstant2 = eslintUtils.getStringIfConstant;
var hasSideEffect2 = eslintUtils.hasSideEffect;
var isParenthesized2 = eslintUtils.isParenthesized;

// vendor/packages/utils/src/ast-utils/eslint-utils/PatternMatcher.ts
var eslintUtils2 = __toESM(require("@eslint-community/eslint-utils"), 1);
var PatternMatcher2 = eslintUtils2.PatternMatcher;

// vendor/packages/utils/src/ast-utils/eslint-utils/predicates.ts
var eslintUtils3 = __toESM(require("@eslint-community/eslint-utils"), 1);
var isArrowToken2 = eslintUtils3.isArrowToken;
var isNotArrowToken2 = eslintUtils3.isNotArrowToken;
var isClosingBraceToken2 = eslintUtils3.isClosingBraceToken;
var isNotClosingBraceToken2 = eslintUtils3.isNotClosingBraceToken;
var isClosingBracketToken2 = eslintUtils3.isClosingBracketToken;
var isNotClosingBracketToken2 = eslintUtils3.isNotClosingBracketToken;
var isClosingParenToken2 = eslintUtils3.isClosingParenToken;
var isNotClosingParenToken2 = eslintUtils3.isNotClosingParenToken;
var isColonToken2 = eslintUtils3.isColonToken;
var isNotColonToken2 = eslintUtils3.isNotColonToken;
var isCommaToken2 = eslintUtils3.isCommaToken;
var isNotCommaToken2 = eslintUtils3.isNotCommaToken;
var isCommentToken2 = eslintUtils3.isCommentToken;
var isNotCommentToken2 = eslintUtils3.isNotCommentToken;
var isOpeningBraceToken2 = eslintUtils3.isOpeningBraceToken;
var isNotOpeningBraceToken2 = eslintUtils3.isNotOpeningBraceToken;
var isOpeningBracketToken2 = eslintUtils3.isOpeningBracketToken;
var isNotOpeningBracketToken2 = eslintUtils3.isNotOpeningBracketToken;
var isOpeningParenToken2 = eslintUtils3.isOpeningParenToken;
var isNotOpeningParenToken2 = eslintUtils3.isNotOpeningParenToken;
var isSemicolonToken2 = eslintUtils3.isSemicolonToken;
var isNotSemicolonToken2 = eslintUtils3.isNotSemicolonToken;

// vendor/packages/utils/src/ast-utils/eslint-utils/ReferenceTracker.ts
var eslintUtils4 = __toESM(require("@eslint-community/eslint-utils"), 1);
var ReferenceTrackerREAD = eslintUtils4.ReferenceTracker.READ;
var ReferenceTrackerCALL = eslintUtils4.ReferenceTracker.CALL;
var ReferenceTrackerCONSTRUCT = eslintUtils4.ReferenceTracker.CONSTRUCT;
var ReferenceTrackerESM = eslintUtils4.ReferenceTracker.ESM;
var ReferenceTracker2 = eslintUtils4.ReferenceTracker;

// vendor/packages/utils/src/ast-utils/eslint-utils/scopeAnalysis.ts
var eslintUtils5 = __toESM(require("@eslint-community/eslint-utils"), 1);
var findVariable2 = eslintUtils5.findVariable;
var getInnermostScope2 = eslintUtils5.getInnermostScope;

// vendor/packages/utils/src/ast-utils/helpers.ts
var isNodeOfType = (nodeType) => (node) => node?.type === nodeType;
var isNodeOfTypes = (nodeTypes) => (node) => !!node && nodeTypes.includes(node.type);
var isNodeOfTypeWithConditions = (nodeType, conditions) => {
  const entries = Object.entries(conditions);
  return (node) => node?.type === nodeType && entries.every(([key, value]) => node[key] === value);
};
var isTokenOfTypeWithConditions = (tokenType, conditions) => {
  const entries = Object.entries(conditions);
  return (token) => token?.type === tokenType && entries.every(
    ([key, value]) => token[key] === value
  );
};
var isNotTokenOfTypeWithConditions = (tokenType, conditions) => (token) => !isTokenOfTypeWithConditions(tokenType, conditions)(token);

// vendor/packages/utils/src/ast-utils/misc.ts
var LINEBREAK_MATCHER = /\r\n|[\r\n\u2028\u2029]/;
function isTokenOnSameLine(left, right) {
  return left.loc.end.line === right.loc.start.line;
}

// vendor/packages/utils/src/ts-estree.ts
var import_types = require("@typescript-eslint/types");

// vendor/packages/utils/src/ast-utils/predicates.ts
var isOptionalChainPunctuator = isTokenOfTypeWithConditions(
  import_types.AST_TOKEN_TYPES.Punctuator,
  { value: "?." }
);
var isNotOptionalChainPunctuator = isNotTokenOfTypeWithConditions(
  import_types.AST_TOKEN_TYPES.Punctuator,
  { value: "?." }
);
var isNonNullAssertionPunctuator = isTokenOfTypeWithConditions(
  import_types.AST_TOKEN_TYPES.Punctuator,
  { value: "!" }
);
var isNotNonNullAssertionPunctuator = isNotTokenOfTypeWithConditions(
  import_types.AST_TOKEN_TYPES.Punctuator,
  { value: "!" }
);
var isOptionalCallExpression = isNodeOfTypeWithConditions(
  import_types.AST_NODE_TYPES.CallExpression,
  // this flag means the call expression itself is option
  // i.e. it is foo.bar?.() and not foo?.bar()
  { optional: true }
);
var isLogicalOrOperator = isNodeOfTypeWithConditions(
  import_types.AST_NODE_TYPES.LogicalExpression,
  { operator: "||" }
);
var isTypeAssertion = isNodeOfTypes([
  import_types.AST_NODE_TYPES.TSAsExpression,
  import_types.AST_NODE_TYPES.TSTypeAssertion
]);
var isVariableDeclarator = isNodeOfType(
  import_types.AST_NODE_TYPES.VariableDeclarator
);
var functionTypes = [
  import_types.AST_NODE_TYPES.ArrowFunctionExpression,
  import_types.AST_NODE_TYPES.FunctionDeclaration,
  import_types.AST_NODE_TYPES.FunctionExpression
];
var isFunction = isNodeOfTypes(functionTypes);
var functionTypeTypes = [
  import_types.AST_NODE_TYPES.TSCallSignatureDeclaration,
  import_types.AST_NODE_TYPES.TSConstructorType,
  import_types.AST_NODE_TYPES.TSConstructSignatureDeclaration,
  import_types.AST_NODE_TYPES.TSDeclareFunction,
  import_types.AST_NODE_TYPES.TSEmptyBodyFunctionExpression,
  import_types.AST_NODE_TYPES.TSFunctionType,
  import_types.AST_NODE_TYPES.TSMethodSignature
];
var isFunctionType = isNodeOfTypes(functionTypeTypes);
var isFunctionOrFunctionType = isNodeOfTypes([
  ...functionTypes,
  ...functionTypeTypes
]);
var isTSFunctionType = isNodeOfType(import_types.AST_NODE_TYPES.TSFunctionType);
var isTSConstructorType = isNodeOfType(
  import_types.AST_NODE_TYPES.TSConstructorType
);
var isClassOrTypeElement = isNodeOfTypes([
  // ClassElement
  import_types.AST_NODE_TYPES.PropertyDefinition,
  import_types.AST_NODE_TYPES.FunctionExpression,
  import_types.AST_NODE_TYPES.MethodDefinition,
  import_types.AST_NODE_TYPES.TSAbstractPropertyDefinition,
  import_types.AST_NODE_TYPES.TSAbstractMethodDefinition,
  import_types.AST_NODE_TYPES.TSEmptyBodyFunctionExpression,
  import_types.AST_NODE_TYPES.TSIndexSignature,
  // TypeElement
  import_types.AST_NODE_TYPES.TSCallSignatureDeclaration,
  import_types.AST_NODE_TYPES.TSConstructSignatureDeclaration,
  // AST_NODE_TYPES.TSIndexSignature,
  import_types.AST_NODE_TYPES.TSMethodSignature,
  import_types.AST_NODE_TYPES.TSPropertySignature
]);
var isConstructor = isNodeOfTypeWithConditions(
  import_types.AST_NODE_TYPES.MethodDefinition,
  { kind: "constructor" }
);
function isSetter(node) {
  return !!node && (node.type === import_types.AST_NODE_TYPES.MethodDefinition || node.type === import_types.AST_NODE_TYPES.Property) && node.kind === "set";
}
var isIdentifier = isNodeOfType(import_types.AST_NODE_TYPES.Identifier);
var isAwaitExpression = isNodeOfType(import_types.AST_NODE_TYPES.AwaitExpression);
var isAwaitKeyword = isTokenOfTypeWithConditions(
  import_types.AST_TOKEN_TYPES.Identifier,
  { value: "await" }
);
var isTypeKeyword = isTokenOfTypeWithConditions(
  import_types.AST_TOKEN_TYPES.Identifier,
  { value: "type" }
);
var isImportKeyword = isTokenOfTypeWithConditions(
  import_types.AST_TOKEN_TYPES.Keyword,
  { value: "import" }
);
var isLoop = isNodeOfTypes([
  import_types.AST_NODE_TYPES.DoWhileStatement,
  import_types.AST_NODE_TYPES.ForStatement,
  import_types.AST_NODE_TYPES.ForInStatement,
  import_types.AST_NODE_TYPES.ForOfStatement,
  import_types.AST_NODE_TYPES.WhileStatement
]);

// vendor/packages/utils/src/eslint-utils/index.ts
var eslint_utils_exports = {};
__export(eslint_utils_exports, {
  NullThrowsReasons: () => NullThrowsReasons,
  RuleCreator: () => RuleCreator,
  nullThrows: () => nullThrows
});

// vendor/packages/utils/src/eslint-utils/deepMerge.ts
function isObjectNotArray(obj) {
  return typeof obj === "object" && obj != null && !Array.isArray(obj);
}
function deepMerge(first = {}, second = {}) {
  const keys = /* @__PURE__ */ new Set([...Object.keys(first), ...Object.keys(second)]);
  return Object.fromEntries(
    [...keys].map((key) => {
      const firstHasKey = key in first;
      const secondHasKey = key in second;
      const firstValue = first[key];
      const secondValue = second[key];
      let value;
      if (firstHasKey && secondHasKey) {
        if (isObjectNotArray(firstValue) && isObjectNotArray(secondValue)) {
          value = deepMerge(firstValue, secondValue);
        } else {
          value = secondValue;
        }
      } else if (firstHasKey) {
        value = firstValue;
      } else {
        value = secondValue;
      }
      return [key, value];
    })
  );
}

// vendor/packages/utils/src/eslint-utils/applyDefault.ts
function applyDefault(defaultOptions, userOptions) {
  const options = structuredClone(defaultOptions);
  if (userOptions == null) {
    return options;
  }
  options.forEach((opt, i) => {
    if (userOptions[i] !== void 0) {
      const userOpt = userOptions[i];
      if (isObjectNotArray(userOpt) && isObjectNotArray(opt)) {
        options[i] = deepMerge(opt, userOpt);
      } else {
        options[i] = userOpt;
      }
    }
  });
  return options;
}

// vendor/packages/utils/src/eslint-utils/RuleCreator.ts
function RuleCreator(urlCreator) {
  return function createNamedRule({
    meta,
    name,
    ...rule
  }) {
    const ruleWithDocs = createRule({
      meta: {
        ...meta,
        docs: {
          ...meta.docs,
          url: urlCreator(name)
        }
      },
      name,
      ...rule
    });
    return ruleWithDocs;
  };
}
function createRule({
  create,
  // Keep accepting deprecated defaultOptions for backward compatibility.
  // eslint-disable-next-line @typescript-eslint/no-deprecated
  defaultOptions,
  meta,
  name
}) {
  const resolvedDefaultOptions = meta.defaultOptions ?? defaultOptions ?? [];
  return {
    create(context) {
      const optionsWithDefault = applyDefault(
        resolvedDefaultOptions,
        context.options
      );
      return create(context, optionsWithDefault);
    },
    // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
    defaultOptions,
    meta,
    name
  };
}
RuleCreator.withoutDocs = function withoutDocs(args) {
  return createRule(args);
};

// vendor/packages/utils/src/eslint-utils/nullThrows.ts
var NullThrowsReasons = {
  MissingParent: "Expected node to have a parent.",
  MissingToken: (token, thing) => `Expected to find a ${token} for the ${thing}.`
};
function nullThrows(value, message) {
  if (value == null) {
    throw new Error(`Non-null Assertion Failed: ${message}`);
  }
  return value;
}

// vendor/packages/utils/src/ts-eslint/index.ts
var ts_eslint_exports = {};
__export(ts_eslint_exports, {
  Scope: () => Scope
});

// vendor/packages/utils/src/ts-eslint/Scope.ts
var scopeManager = __toESM(require("@typescript-eslint/scope-manager"), 1);
var Scope;
((Scope2) => {
  Scope2.ScopeType = scopeManager.ScopeType;
  Scope2.DefinitionType = scopeManager.DefinitionType;
})(Scope || (Scope = {}));

// vendor/packages/utils/src/index.ts
var import_types2 = require("@typescript-eslint/types");

// vendor/packages/eslint-plugin/src/util/createRule.ts
var createRule2 = eslint_utils_exports.RuleCreator(
  (name) => `https://typescript-eslint.io/rules/${name}`
);

// vendor/packages/eslint-plugin/src/util/collectUnusedVariables.ts
var import_scope_manager3 = require("@typescript-eslint/scope-manager");

// vendor/packages/eslint-plugin/src/util/isTypeImport.ts
var import_scope_manager = require("@typescript-eslint/scope-manager");
function isTypeImport(definition) {
  return definition?.type === import_scope_manager.DefinitionType.ImportBinding && (definition.parent.importKind === "type" || definition.node.type === import_types2.AST_NODE_TYPES.ImportSpecifier && definition.node.importKind === "type");
}

// vendor/packages/eslint-plugin/src/util/isTypeOnlyReference.ts
var import_scope_manager2 = require("@typescript-eslint/scope-manager");

// vendor/packages/eslint-plugin/src/util/referenceContainsTypePredicate.ts
function referenceContainsTypePredicate(node) {
  switch (node.type) {
    case import_types2.AST_NODE_TYPES.TSTypePredicate:
      return true;
    case import_types2.AST_NODE_TYPES.TSQualifiedName:
    case import_types2.AST_NODE_TYPES.Identifier:
      return referenceContainsTypePredicate(node.parent);
    default:
      return false;
  }
}

// vendor/packages/eslint-plugin/src/util/referenceContainsTypeQuery.ts
function referenceContainsTypeQuery(node) {
  switch (node.type) {
    case import_types2.AST_NODE_TYPES.TSTypeQuery:
      return true;
    case import_types2.AST_NODE_TYPES.TSQualifiedName:
    case import_types2.AST_NODE_TYPES.Identifier:
      return referenceContainsTypeQuery(node.parent);
    default:
      return false;
  }
}

// vendor/packages/eslint-plugin/src/util/isTypeOnlyReference.ts
function isMergedTypeValueVariable(variable) {
  return "isTypeVariable" in variable && "isValueVariable" in variable && variable.isTypeVariable && variable.isValueVariable;
}
function isTypeOnlyReference(variable, ref) {
  if (referenceContainsTypeQuery(ref.identifier) || referenceContainsTypePredicate(ref.identifier)) {
    return true;
  }
  return variable.defs.some((def) => def.type === import_scope_manager2.DefinitionType.Variable) && // eslint-disable-next-line @typescript-eslint/no-unnecessary-boolean-literal-compare -- no-unused-vars is one of our rare rules that's used across JS code that is not parsed by our parser. As such `isValueReference` may be undefined and using `!isValueReference` causes false-positives for those users.
  ref.isValueReference === false;
}

// vendor/packages/eslint-plugin/src/util/collectUnusedVariables.ts
var UnusedVarsVisitor = class extends import_scope_manager3.Visitor {
  /**
   * We keep a weak cache so that multiple rules can share the calculation
   */
  static RESULTS_CACHE = /* @__PURE__ */ new WeakMap();
  ClassDeclaration = this.visitClass;
  ClassExpression = this.visitClass;
  ForInStatement = this.visitForInForOf;
  ForOfStatement = this.visitForInForOf;
  //#region HELPERS
  FunctionDeclaration = this.visitFunction;
  FunctionExpression = this.visitFunction;
  MethodDefinition = this.visitSetter;
  Property = this.visitSetter;
  TSCallSignatureDeclaration = this.visitFunctionTypeSignature;
  TSConstructorType = this.visitFunctionTypeSignature;
  TSConstructSignatureDeclaration = this.visitFunctionTypeSignature;
  TSDeclareFunction = this.visitFunctionTypeSignature;
  TSEmptyBodyFunctionExpression = this.visitFunctionTypeSignature;
  //#endregion HELPERS
  //#region VISITORS
  // NOTE - This is a simple visitor - meaning it does not support selectors
  TSFunctionType = this.visitFunctionTypeSignature;
  TSMethodSignature = this.visitFunctionTypeSignature;
  #scopeManager;
  constructor(scopeManager2) {
    super({
      visitChildrenEvenIfSelectorExists: true
    });
    this.#scopeManager = scopeManager2;
  }
  static collectUnusedVariables(program, scopeManager2) {
    const cached = this.RESULTS_CACHE.get(program);
    if (cached) {
      return cached;
    }
    const visitor = new this(scopeManager2);
    visitor.visit(program);
    const unusedVars = visitor.collectUnusedVariables(
      visitor.getScope(program)
    );
    this.RESULTS_CACHE.set(program, unusedVars);
    return unusedVars;
  }
  Identifier(node) {
    const scope = this.getScope(node);
    if (scope.type === ts_eslint_exports.Scope.ScopeType.function && node.name === "this" && // this parameters should always be considered used as they're pseudo-parameters
    "params" in scope.block && scope.block.params.includes(node)) {
      this.markVariableAsUsed(node);
    }
  }
  TSEnumDeclaration(node) {
    const scope = this.getScope(node);
    for (const variable of scope.variables) {
      this.markVariableAsUsed(variable);
    }
  }
  TSMappedType(node) {
    this.markVariableAsUsed(node.key);
  }
  TSModuleDeclaration(node) {
    if (node.kind === "global") {
      this.markVariableAsUsed("global", node.parent);
    }
  }
  TSParameterProperty(node) {
    let identifier;
    switch (node.parameter.type) {
      case import_types2.AST_NODE_TYPES.AssignmentPattern:
        identifier = node.parameter.left;
        break;
      case import_types2.AST_NODE_TYPES.Identifier:
        identifier = node.parameter;
        break;
    }
    this.markVariableAsUsed(identifier);
  }
  collectUnusedVariables(scope, variables = {
    unusedVariables: /* @__PURE__ */ new Set(),
    usedVariables: /* @__PURE__ */ new Set()
  }) {
    if (
      // skip function expression names
      // this scope is created just to house the variable that allows a function
      // expression to self-reference if it has a name defined
      !scope.functionExpressionScope
    ) {
      for (const variable of scope.variables) {
        if (
          // implicit lib variables (from @typescript-eslint/scope-manager)
          // these aren't variables that should be checked ever
          variable instanceof import_scope_manager3.ImplicitLibVariable
        ) {
          continue;
        }
        if (
          // variables marked with markVariableAsUsed()
          variable.eslintUsed || // basic exported variables
          isExported(variable) || // variables implicitly exported via a merged declaration
          isMergeableExported(variable) || // used variables
          isUsedVariable(variable)
        ) {
          variables.usedVariables.add(variable);
        } else {
          variables.unusedVariables.add(variable);
        }
      }
    }
    for (const childScope of scope.childScopes) {
      this.collectUnusedVariables(childScope, variables);
    }
    return variables;
  }
  getScope(currentNode) {
    const inner = currentNode.type !== import_types2.AST_NODE_TYPES.Program;
    let node = currentNode;
    while (node) {
      const scope = this.#scopeManager.acquire(node, inner);
      if (scope) {
        if (scope.type === import_scope_manager3.ScopeType.functionExpressionName) {
          return scope.childScopes[0];
        }
        return scope;
      }
      node = node.parent;
    }
    return this.#scopeManager.scopes[0];
  }
  markVariableAsUsed(variableOrIdentifierOrName, parent) {
    if (typeof variableOrIdentifierOrName !== "string" && !("type" in variableOrIdentifierOrName)) {
      variableOrIdentifierOrName.eslintUsed = true;
      return;
    }
    let name;
    let node;
    if (typeof variableOrIdentifierOrName === "string") {
      name = variableOrIdentifierOrName;
      node = parent;
    } else {
      name = variableOrIdentifierOrName.name;
      node = variableOrIdentifierOrName;
    }
    let currentScope = this.getScope(node);
    while (currentScope) {
      const variable = currentScope.variables.find(
        (scopeVar) => scopeVar.name === name
      );
      if (variable) {
        variable.eslintUsed = true;
        return;
      }
      currentScope = currentScope.upper;
    }
  }
  visitClass(node) {
    const scope = this.getScope(node);
    for (const variable of scope.variables) {
      if (variable.identifiers[0] === scope.block.id) {
        this.markVariableAsUsed(variable);
        return;
      }
    }
  }
  visitForInForOf(node) {
    let idOrVariable;
    if (node.left.type === import_types2.AST_NODE_TYPES.VariableDeclaration) {
      const variable = this.#scopeManager.getDeclaredVariables(node.left).at(0);
      if (!variable) {
        return;
      }
      idOrVariable = variable;
    }
    if (node.left.type === import_types2.AST_NODE_TYPES.Identifier) {
      idOrVariable = node.left;
    }
    if (idOrVariable == null) {
      return;
    }
    let body = node.body;
    if (node.body.type === import_types2.AST_NODE_TYPES.BlockStatement) {
      if (node.body.body.length !== 1) {
        return;
      }
      body = node.body.body[0];
    }
    if (body.type !== import_types2.AST_NODE_TYPES.ReturnStatement) {
      return;
    }
    this.markVariableAsUsed(idOrVariable);
  }
  visitFunction(node) {
    const scope = this.getScope(node);
    const variable = scope.set.get("arguments");
    if (variable?.defs.length === 0) {
      this.markVariableAsUsed(variable);
    }
  }
  visitFunctionTypeSignature(node) {
    for (const param of node.params) {
      this.visitPattern(param, (name) => {
        this.markVariableAsUsed(name);
      });
    }
  }
  visitSetter(node) {
    if (node.kind === "set") {
      for (const param of node.value.params) {
        this.visitPattern(param, (id) => {
          this.markVariableAsUsed(id);
        });
      }
    }
  }
  //#endregion VISITORS
};
function isInside(inner, outer) {
  return inner.range[0] >= outer.range[0] && inner.range[1] <= outer.range[1];
}
function isSelfReference(ref, nodes) {
  let scope = ref.from;
  while (scope) {
    if (nodes.has(scope.block)) {
      return true;
    }
    scope = scope.upper;
  }
  return false;
}
var MERGEABLE_TYPES = /* @__PURE__ */ new Set([
  import_types2.AST_NODE_TYPES.ClassDeclaration,
  import_types2.AST_NODE_TYPES.FunctionDeclaration,
  import_types2.AST_NODE_TYPES.TSInterfaceDeclaration,
  import_types2.AST_NODE_TYPES.TSModuleDeclaration,
  import_types2.AST_NODE_TYPES.TSTypeAliasDeclaration
]);
function isMergedTypeDeclaration(variable, node) {
  return (node.type === import_types2.AST_NODE_TYPES.TSTypeAliasDeclaration || node.type === import_types2.AST_NODE_TYPES.TSInterfaceDeclaration) && isMergedTypeValueVariable(variable);
}
function isMergeableExported(variable) {
  for (const def of variable.defs) {
    if (def.type === ts_eslint_exports.Scope.DefinitionType.Parameter) {
      continue;
    }
    if (MERGEABLE_TYPES.has(def.node.type) && def.node.parent.type === import_types2.AST_NODE_TYPES.ExportNamedDeclaration || def.node.parent.type === import_types2.AST_NODE_TYPES.ExportDefaultDeclaration) {
      return !isMergedTypeDeclaration(variable, def.node);
    }
  }
  return false;
}
function isExported(variable) {
  return variable.defs.some((definition) => {
    let node = definition.node;
    if (node.type === import_types2.AST_NODE_TYPES.VariableDeclarator) {
      node = node.parent;
    } else if (definition.type === ts_eslint_exports.Scope.DefinitionType.Parameter) {
      return false;
    }
    return !isMergedTypeDeclaration(variable, node) && node.parent.type.startsWith("Export");
  });
}
var LOGICAL_ASSIGNMENT_OPERATORS = /* @__PURE__ */ new Set(["??=", "&&=", "||="]);
function isUsedVariable(variable) {
  function getFunctionDefinitions(variable2) {
    const functionDefinitions = /* @__PURE__ */ new Set();
    variable2.defs.forEach((def) => {
      if (def.type === ts_eslint_exports.Scope.DefinitionType.FunctionName) {
        functionDefinitions.add(def.node);
      }
      if (def.type === ts_eslint_exports.Scope.DefinitionType.Variable && (def.node.init?.type === import_types2.AST_NODE_TYPES.FunctionExpression || def.node.init?.type === import_types2.AST_NODE_TYPES.ArrowFunctionExpression)) {
        functionDefinitions.add(def.node.init);
      }
    });
    return functionDefinitions;
  }
  function getTypeDeclarations(variable2) {
    const nodes = /* @__PURE__ */ new Set();
    variable2.defs.forEach((def) => {
      if (def.node.type === import_types2.AST_NODE_TYPES.TSInterfaceDeclaration || def.node.type === import_types2.AST_NODE_TYPES.TSTypeAliasDeclaration) {
        nodes.add(def.node);
      }
    });
    return nodes;
  }
  function getModuleDeclarations(variable2) {
    const nodes = /* @__PURE__ */ new Set();
    variable2.defs.forEach((def) => {
      if (def.node.type === import_types2.AST_NODE_TYPES.TSModuleDeclaration) {
        nodes.add(def.node);
      }
    });
    return nodes;
  }
  function getEnumDeclarations(variable2) {
    const nodes = /* @__PURE__ */ new Set();
    variable2.defs.forEach((def) => {
      if (def.node.type === import_types2.AST_NODE_TYPES.TSEnumDeclaration) {
        nodes.add(def.node);
      }
    });
    return nodes;
  }
  function isInsideOneOf(ref, nodes) {
    for (const node of nodes) {
      if (isInside(ref.identifier, node)) {
        return true;
      }
    }
    return false;
  }
  function isUnusedExpression(node) {
    const parent = node.parent;
    if (parent.type === import_types2.AST_NODE_TYPES.ExpressionStatement) {
      return true;
    }
    if (parent.type === import_types2.AST_NODE_TYPES.SequenceExpression) {
      const isLastExpression = parent.expressions[parent.expressions.length - 1] === node;
      if (!isLastExpression) {
        return true;
      }
      return isUnusedExpression(parent);
    }
    return false;
  }
  function getRhsNode(ref, prevRhsNode) {
    function isInLoop(node) {
      let currentNode = node;
      while (currentNode) {
        if (ast_utils_exports.isFunction(currentNode)) {
          break;
        }
        if (ast_utils_exports.isLoop(currentNode)) {
          return true;
        }
        currentNode = currentNode.parent;
      }
      return false;
    }
    const id = ref.identifier;
    const parent = id.parent;
    const refScope = ref.from.variableScope;
    const varScope = ref.resolved.scope.variableScope;
    const canBeUsedLater = refScope !== varScope || isInLoop(id);
    if (prevRhsNode && isInside(id, prevRhsNode)) {
      return prevRhsNode;
    }
    if (parent.type === import_types2.AST_NODE_TYPES.AssignmentExpression && isUnusedExpression(parent) && id === parent.left && !canBeUsedLater) {
      return parent.right;
    }
    return null;
  }
  function isReadForItself(ref, rhsNode2) {
    function isInsideOfStorableFunction(id2, rhsNode3) {
      function getUpperFunction(node) {
        let currentNode = node;
        while (currentNode) {
          if (ast_utils_exports.isFunction(currentNode)) {
            return currentNode;
          }
          currentNode = currentNode.parent;
        }
        return null;
      }
      function isStorableFunction(funcNode2, rhsNode4) {
        let node = funcNode2;
        let parent2 = funcNode2.parent;
        while (parent2 && isInside(parent2, rhsNode4)) {
          switch (parent2.type) {
            case import_types2.AST_NODE_TYPES.SequenceExpression:
              if (parent2.expressions[parent2.expressions.length - 1] !== node) {
                return false;
              }
              break;
            case import_types2.AST_NODE_TYPES.CallExpression:
            case import_types2.AST_NODE_TYPES.NewExpression:
              return parent2.callee !== node;
            case import_types2.AST_NODE_TYPES.AssignmentExpression:
            case import_types2.AST_NODE_TYPES.TaggedTemplateExpression:
            case import_types2.AST_NODE_TYPES.YieldExpression:
              return true;
            default:
              if (parent2.type.endsWith("Statement") || parent2.type.endsWith("Declaration")) {
                return true;
              }
          }
          node = parent2;
          parent2 = parent2.parent;
        }
        return false;
      }
      const funcNode = getUpperFunction(id2);
      return !!funcNode && isInside(funcNode, rhsNode3) && isStorableFunction(funcNode, rhsNode3);
    }
    const id = ref.identifier;
    const parent = id.parent;
    return ref.isRead() && // in RHS of an assignment for itself. e.g. `a = a + 1`
    // self update. e.g. `a += 1`, `a++`
    (parent.type === import_types2.AST_NODE_TYPES.AssignmentExpression && !LOGICAL_ASSIGNMENT_OPERATORS.has(parent.operator) && isUnusedExpression(parent) && parent.left === id || parent.type === import_types2.AST_NODE_TYPES.UpdateExpression && isUnusedExpression(parent) || !!rhsNode2 && isInside(id, rhsNode2) && !isInsideOfStorableFunction(id, rhsNode2));
  }
  const functionNodes = getFunctionDefinitions(variable);
  const isFunctionDefinition = functionNodes.size > 0;
  const typeDeclNodes = getTypeDeclarations(variable);
  const isTypeDecl = typeDeclNodes.size > 0;
  const moduleDeclNodes = getModuleDeclarations(variable);
  const isModuleDecl = moduleDeclNodes.size > 0;
  const enumDeclNodes = getEnumDeclarations(variable);
  const isEnumDecl = enumDeclNodes.size > 0;
  const isImportedAsType = variable.defs.every(isTypeImport);
  let rhsNode = null;
  return variable.references.some((ref) => {
    const forItself = isReadForItself(ref, rhsNode);
    rhsNode = getRhsNode(ref, rhsNode);
    return ref.isRead() && !forItself && !(!isImportedAsType && isTypeOnlyReference(variable, ref)) && !(isFunctionDefinition && isSelfReference(ref, functionNodes)) && !(isTypeDecl && isInsideOneOf(ref, typeDeclNodes)) && !(isModuleDecl && isSelfReference(ref, moduleDeclNodes)) && !(isEnumDecl && isSelfReference(ref, enumDeclNodes));
  });
}
function collectVariables(context) {
  return UnusedVarsVisitor.collectUnusedVariables(
    context.sourceCode.ast,
    eslint_utils_exports.nullThrows(
      context.sourceCode.scopeManager,
      "Missing required scope manager"
    )
  );
}

// vendor/packages/eslint-plugin/src/util/escapeRegExp.ts
var reRegExpChar = /[\\^$.*+?()[\]{}|]/g;
var reHasRegExpChar = RegExp(reRegExpChar.source);
function escapeRegExp(string = "") {
  return string && reHasRegExpChar.test(string) ? string.replaceAll(reRegExpChar, "\\$&") : string;
}

// vendor/packages/eslint-plugin/src/util/astUtils.ts
function getNameLocationInGlobalDirectiveComment(sourceCode, comment, name) {
  const namePattern = new RegExp(
    `[\\s,]${escapeRegExp(name)}(?:$|[\\s,:])`,
    "gu"
  );
  namePattern.lastIndex = comment.value.indexOf("global") + 6;
  const match = namePattern.exec(comment.value);
  const start = sourceCode.getLocFromIndex(
    comment.range[0] + "/*".length + (match ? match.index + 1 : 0)
  );
  const end = {
    column: start.column + (match ? name.length : 1),
    line: start.line
  };
  return { end, start };
}

// vendor/packages/eslint-plugin/src/util/isNullLiteral.ts
function isNullLiteral(i) {
  return i.type === import_types2.AST_NODE_TYPES.Literal && i.value == null;
}

// vendor/packages/eslint-plugin/src/util/getStaticStringValue.ts
function getStaticStringValue(node) {
  switch (node.type) {
    case import_types2.AST_NODE_TYPES.Literal:
      if (node.value === null) {
        if (isNullLiteral(node)) {
          return String(node.value);
        }
        if ("regex" in node) {
          return `/${node.regex.pattern}/${node.regex.flags}`;
        }
        if ("bigint" in node) {
          return node.bigint;
        }
      } else {
        return String(node.value);
      }
      break;
    case import_types2.AST_NODE_TYPES.TemplateLiteral:
      if (node.expressions.length === 0 && node.quasis.length === 1) {
        return node.quasis[0].value.cooked;
      }
      break;
  }
  return null;
}

// vendor/packages/eslint-plugin/src/util/misc.ts
var DEFINITION_EXTENSIONS = [".d.ts", ".d.cts", ".d.mts"];
function isDefinitionFile(fileName) {
  const lowerFileName = fileName.toLowerCase();
  for (const definitionExt of DEFINITION_EXTENSIONS) {
    if (lowerFileName.endsWith(definitionExt)) {
      return true;
    }
  }
  return /\.d\.(ts|cts|mts|.*\.ts)$/.test(lowerFileName);
}

// vendor/packages/eslint-plugin/src/rules/no-array-constructor.ts
var no_array_constructor_default = createRule2({
  name: "no-array-constructor",
  meta: {
    type: "suggestion",
    docs: {
      description: "Disallow generic `Array` constructors",
      extendsBaseRule: true,
      recommended: "recommended"
    },
    fixable: "code",
    messages: {
      useLiteral: "The array literal notation [] is preferable."
    },
    schema: []
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [],
  create(context) {
    const sourceCode = context.sourceCode;
    function getArgumentsText(node) {
      const lastToken = sourceCode.getLastToken(node);
      if (lastToken == null || !isClosingParenToken2(lastToken)) {
        return "";
      }
      let firstToken = node.callee;
      do {
        firstToken = sourceCode.getTokenAfter(firstToken);
        if (!firstToken || firstToken === lastToken) {
          return "";
        }
      } while (!isOpeningParenToken2(firstToken));
      return sourceCode.text.slice(firstToken.range[1], lastToken.range[0]);
    }
    function check(node) {
      if (node.arguments.length !== 1 && node.callee.type === import_types2.AST_NODE_TYPES.Identifier && node.callee.name === "Array" && !node.typeArguments) {
        context.report({
          node,
          messageId: "useLiteral",
          fix(fixer) {
            const argsText = getArgumentsText(node);
            return fixer.replaceText(node, `[${argsText}]`);
          }
        });
      }
    }
    return {
      CallExpression: check,
      NewExpression: check
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/no-duplicate-enum-values.ts
var no_duplicate_enum_values_default = createRule2({
  name: "no-duplicate-enum-values",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow duplicate enum member values",
      recommended: "recommended"
    },
    hasSuggestions: false,
    messages: {
      duplicateValue: "Duplicate enum member value {{value}}."
    },
    schema: []
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [],
  create(context) {
    function isStringLiteral(node) {
      return node.type === import_types2.AST_NODE_TYPES.Literal && typeof node.value === "string";
    }
    function isNumberLiteral(node) {
      return node.type === import_types2.AST_NODE_TYPES.Literal && typeof node.value === "number";
    }
    function isSupportedUnary(node) {
      return node.type === import_types2.AST_NODE_TYPES.UnaryExpression && ["-", "+"].includes(node.operator);
    }
    function isStaticTemplateLiteral(node) {
      return node.type === import_types2.AST_NODE_TYPES.TemplateLiteral && node.expressions.length === 0 && node.quasis.length === 1;
    }
    function getMemberValue(initializer) {
      switch (true) {
        case isStringLiteral(initializer):
        case isNumberLiteral(initializer):
          return initializer.value;
        case isSupportedUnary(initializer): {
          const inner = Number(getMemberValue(initializer.argument));
          if (Number.isNaN(inner)) {
            return void 0;
          }
          return initializer.operator === "-" ? -inner : inner;
        }
        case isStaticTemplateLiteral(initializer):
          return initializer.quasis[0].value.cooked ?? void 0;
        default:
          return void 0;
      }
    }
    return {
      TSEnumDeclaration(node) {
        const enumMembers = node.body.members;
        const seenValues = [];
        enumMembers.forEach((member) => {
          if (member.initializer == null) {
            return;
          }
          const value = getMemberValue(member.initializer);
          if (value == null) {
            return;
          }
          const isAlreadyPresent = seenValues.some(
            (seenValue) => Object.is(seenValue, value)
          );
          if (isAlreadyPresent) {
            context.report({
              node: member,
              messageId: "duplicateValue",
              data: {
                value
              }
            });
          } else {
            seenValues.push(value);
          }
        });
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/no-extra-non-null-assertion.ts
var no_extra_non_null_assertion_default = createRule2({
  name: "no-extra-non-null-assertion",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow extra non-null assertions",
      recommended: "recommended"
    },
    fixable: "code",
    messages: {
      noExtraNonNullAssertion: "Forbidden extra non-null assertion."
    },
    schema: []
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [],
  create(context) {
    function checkExtraNonNullAssertion(node) {
      context.report({
        node,
        messageId: "noExtraNonNullAssertion",
        fix(fixer) {
          return fixer.removeRange([node.range[1] - 1, node.range[1]]);
        }
      });
    }
    return {
      "CallExpression[optional = true] > TSNonNullExpression.callee": checkExtraNonNullAssertion,
      "MemberExpression[optional = true] > TSNonNullExpression.object": checkExtraNonNullAssertion,
      "TSNonNullExpression > TSNonNullExpression": checkExtraNonNullAssertion
    };
  }
});

// vendor/packages/eslint-plugin/src/util/getESLintCoreRule.ts
var import_use_at_your_own_risk = require("eslint/use-at-your-own-risk");
var getESLintCoreRule = (ruleId) => eslint_utils_exports.nullThrows(
  import_use_at_your_own_risk.builtinRules.get(ruleId),
  `ESLint's core rule '${ruleId}' not found.`
);

// vendor/packages/eslint-plugin/src/rules/no-loss-of-precision.ts
var baseRule = getESLintCoreRule(
  "no-loss-of-precision"
);
var no_loss_of_precision_default = createRule2({
  name: "no-loss-of-precision",
  meta: {
    type: "problem",
    // defaultOptions, -- base rule does not use defaultOptions
    deprecated: {
      deprecatedSince: "8.0.0",
      replacedBy: [
        {
          rule: {
            name: "no-loss-of-precision",
            url: "https://eslint.org/docs/latest/rules/no-loss-of-precision"
          }
        }
      ],
      url: "https://github.com/typescript-eslint/typescript-eslint/pull/8832"
    },
    docs: {
      description: "Disallow literal numbers that lose precision",
      extendsBaseRule: true
    },
    hasSuggestions: baseRule.meta.hasSuggestions,
    messages: baseRule.meta.messages,
    schema: []
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [],
  create(context) {
    return baseRule.create(context);
  }
});

// vendor/packages/eslint-plugin/src/rules/no-misused-new.ts
var no_misused_new_default = createRule2({
  name: "no-misused-new",
  meta: {
    type: "problem",
    docs: {
      description: "Enforce valid definition of `new` and `constructor`",
      recommended: "recommended"
    },
    messages: {
      errorMessageClass: "Class cannot have method named `new`.",
      errorMessageInterface: "Interfaces cannot be constructed, only classes."
    },
    schema: []
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [],
  create(context) {
    function getTypeReferenceName(node) {
      if (node) {
        switch (node.type) {
          case import_types2.AST_NODE_TYPES.TSTypeAnnotation:
            return getTypeReferenceName(node.typeAnnotation);
          case import_types2.AST_NODE_TYPES.TSTypeReference:
            return getTypeReferenceName(node.typeName);
          case import_types2.AST_NODE_TYPES.Identifier:
            return node.name;
          default:
            break;
        }
      }
      return null;
    }
    function isMatchingParentType(parent, returnType) {
      if (parent && (parent.type === import_types2.AST_NODE_TYPES.ClassDeclaration || parent.type === import_types2.AST_NODE_TYPES.ClassExpression || parent.type === import_types2.AST_NODE_TYPES.TSInterfaceDeclaration) && parent.id) {
        return getTypeReferenceName(returnType) === parent.id.name;
      }
      return false;
    }
    return {
      "ClassBody > MethodDefinition[key.name='new']"(node) {
        if (node.value.type === import_types2.AST_NODE_TYPES.TSEmptyBodyFunctionExpression && isMatchingParentType(node.parent.parent, node.value.returnType)) {
          context.report({
            node,
            messageId: "errorMessageClass"
          });
        }
      },
      "TSInterfaceBody > TSConstructSignatureDeclaration"(node) {
        if (isMatchingParentType(
          node.parent.parent,
          node.returnType
        )) {
          context.report({
            node,
            messageId: "errorMessageInterface"
          });
        }
      },
      "TSMethodSignature[key.name='constructor']"(node) {
        context.report({
          node,
          messageId: "errorMessageInterface"
        });
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/no-namespace.ts
var no_namespace_default = createRule2({
  name: "no-namespace",
  meta: {
    type: "suggestion",
    docs: {
      description: "Disallow TypeScript namespaces",
      recommended: "recommended"
    },
    messages: {
      moduleSyntaxIsPreferred: "ES2015 module syntax is preferred over namespaces."
    },
    schema: [
      {
        type: "object",
        additionalProperties: false,
        properties: {
          allowDeclarations: {
            type: "boolean",
            description: "Whether to allow `declare` with custom TypeScript namespaces."
          },
          allowDefinitionFiles: {
            type: "boolean",
            description: "Whether to allow `declare` with custom TypeScript namespaces inside definition files."
          }
        }
      }
    ]
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [
    {
      allowDeclarations: false,
      allowDefinitionFiles: true
    }
  ],
  create(context, [{ allowDeclarations, allowDefinitionFiles }]) {
    function isDeclaration(node) {
      if (node.type === import_types2.AST_NODE_TYPES.TSModuleDeclaration && node.declare) {
        return true;
      }
      return node.parent != null && isDeclaration(node.parent);
    }
    return {
      "TSModuleDeclaration[global!=true][id.type!='Literal']"(node) {
        if (node.parent.type === import_types2.AST_NODE_TYPES.TSModuleDeclaration || allowDefinitionFiles && isDefinitionFile(context.filename) || allowDeclarations && isDeclaration(node)) {
          return;
        }
        context.report({
          node,
          messageId: "moduleSyntaxIsPreferred"
        });
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/no-non-null-asserted-optional-chain.ts
var no_non_null_asserted_optional_chain_default = createRule2({
  name: "no-non-null-asserted-optional-chain",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow non-null assertions after an optional chain expression",
      recommended: "recommended"
    },
    hasSuggestions: true,
    messages: {
      noNonNullOptionalChain: "Optional chain expressions can return undefined by design - using a non-null assertion is unsafe and wrong.",
      suggestRemovingNonNull: "You should remove the non-null assertion."
    },
    schema: []
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [],
  create(context) {
    return {
      // non-nulling a wrapped chain will scrub all nulls introduced by the chain
      // (x?.y)!
      // (x?.())!
      "TSNonNullExpression > ChainExpression"(node) {
        const parent = node.parent;
        context.report({
          node,
          messageId: "noNonNullOptionalChain",
          // use a suggestion instead of a fixer, because this can obviously break type checks
          suggest: [
            {
              messageId: "suggestRemovingNonNull",
              fix(fixer) {
                return fixer.removeRange([
                  parent.range[1] - 1,
                  parent.range[1]
                ]);
              }
            }
          ]
        });
      },
      // non-nulling at the end of a chain will scrub all nulls introduced by the chain
      // x?.y!
      // x?.()!
      "ChainExpression > TSNonNullExpression"(node) {
        context.report({
          node,
          messageId: "noNonNullOptionalChain",
          // use a suggestion instead of a fixer, because this can obviously break type checks
          suggest: [
            {
              messageId: "suggestRemovingNonNull",
              fix(fixer) {
                return fixer.removeRange([node.range[1] - 1, node.range[1]]);
              }
            }
          ]
        });
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/no-this-alias.ts
var no_this_alias_default = createRule2({
  name: "no-this-alias",
  meta: {
    type: "suggestion",
    docs: {
      description: "Disallow aliasing `this`",
      recommended: "recommended"
    },
    messages: {
      thisAssignment: "Unexpected aliasing of 'this' to local variable.",
      thisDestructure: "Unexpected aliasing of members of 'this' to local variables."
    },
    schema: [
      {
        type: "object",
        additionalProperties: false,
        properties: {
          allowDestructuring: {
            type: "boolean",
            description: "Whether to ignore destructuring, such as `const { props, state } = this`."
          },
          allowedNames: {
            type: "array",
            description: 'Names to ignore, such as ["self"] for `const self = this;`.',
            items: {
              type: "string"
            }
          }
        }
      }
    ]
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [
    {
      allowDestructuring: true,
      allowedNames: []
    }
  ],
  create(context, [{ allowDestructuring, allowedNames }]) {
    return {
      "VariableDeclarator[init.type='ThisExpression'], AssignmentExpression[right.type='ThisExpression']"(node) {
        const id = node.type === import_types2.AST_NODE_TYPES.VariableDeclarator ? node.id : node.left;
        if (allowDestructuring && id.type !== import_types2.AST_NODE_TYPES.Identifier) {
          return;
        }
        const hasAllowedName = id.type === import_types2.AST_NODE_TYPES.Identifier ? (
          // https://github.com/typescript-eslint/typescript-eslint/issues/5439
          // eslint-disable-next-line @typescript-eslint/no-non-null-assertion
          allowedNames.includes(id.name)
        ) : false;
        if (!hasAllowedName) {
          context.report({
            node: id,
            messageId: id.type === import_types2.AST_NODE_TYPES.Identifier ? "thisAssignment" : "thisDestructure"
          });
        }
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/no-unnecessary-type-constraint.ts
var import_node_path = require("node:path");
var no_unnecessary_type_constraint_default = createRule2({
  name: "no-unnecessary-type-constraint",
  meta: {
    type: "suggestion",
    docs: {
      description: "Disallow unnecessary constraints on generic types",
      recommended: "recommended"
    },
    hasSuggestions: true,
    messages: {
      removeUnnecessaryConstraint: "Remove the unnecessary `{{constraint}}` constraint.",
      unnecessaryConstraint: "Constraining the generic type `{{name}}` to `{{constraint}}` does nothing and is unnecessary."
    },
    schema: []
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [],
  create(context) {
    const unnecessaryConstraints = /* @__PURE__ */ new Map([
      [import_types2.AST_NODE_TYPES.TSAnyKeyword, "any"],
      [import_types2.AST_NODE_TYPES.TSUnknownKeyword, "unknown"]
    ]);
    function checkRequiresGenericDeclarationDisambiguation(filename) {
      const pathExt = (0, import_node_path.extname)(filename).toLocaleLowerCase();
      switch (pathExt) {
        case ".cts":
        case ".mts":
        case ".tsx":
          return true;
        default:
          return false;
      }
    }
    const requiresGenericDeclarationDisambiguation = checkRequiresGenericDeclarationDisambiguation(context.filename);
    const checkNode = (node, inArrowFunction) => {
      const constraint = unnecessaryConstraints.get(node.constraint.type);
      function shouldAddTrailingComma() {
        if (!inArrowFunction || !requiresGenericDeclarationDisambiguation) {
          return false;
        }
        return node.parent.params.length === 1 && context.sourceCode.getTokensAfter(node)[0].value !== "," && !node.default;
      }
      if (constraint) {
        context.report({
          node,
          messageId: "unnecessaryConstraint",
          data: {
            name: node.name.name,
            constraint
          },
          suggest: [
            {
              messageId: "removeUnnecessaryConstraint",
              data: {
                constraint
              },
              fix(fixer) {
                return fixer.replaceTextRange(
                  [node.name.range[1], node.constraint.range[1]],
                  shouldAddTrailingComma() ? "," : ""
                );
              }
            }
          ]
        });
      }
    };
    return {
      ":not(ArrowFunctionExpression) > TSTypeParameterDeclaration > TSTypeParameter[constraint]"(node) {
        checkNode(node, false);
      },
      "ArrowFunctionExpression > TSTypeParameterDeclaration > TSTypeParameter[constraint]"(node) {
        checkNode(node, true);
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/no-unsafe-declaration-merging.ts
var no_unsafe_declaration_merging_default = createRule2({
  name: "no-unsafe-declaration-merging",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow unsafe declaration merging",
      recommended: "recommended",
      requiresTypeChecking: false
    },
    messages: {
      unsafeMerging: "Unsafe declaration merging between classes and interfaces."
    },
    schema: []
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [],
  create(context) {
    function checkUnsafeDeclaration(scope, node, unsafeKind) {
      const variable = scope.set.get(node.name);
      if (!variable) {
        return;
      }
      const defs = variable.defs;
      if (defs.length <= 1) {
        return;
      }
      if (defs.some((def) => def.node.type === unsafeKind)) {
        context.report({
          node,
          messageId: "unsafeMerging"
        });
      }
    }
    return {
      ClassDeclaration(node) {
        if (node.id) {
          const currentScope = context.sourceCode.getScope(node).upper;
          if (currentScope == null) {
            return;
          }
          checkUnsafeDeclaration(
            currentScope,
            node.id,
            import_types2.AST_NODE_TYPES.TSInterfaceDeclaration
          );
        }
      },
      TSInterfaceDeclaration(node) {
        checkUnsafeDeclaration(
          context.sourceCode.getScope(node),
          node.id,
          import_types2.AST_NODE_TYPES.ClassDeclaration
        );
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/no-unused-vars.ts
var import_scope_manager4 = require("@typescript-eslint/scope-manager");
var isCommaToken3 = {
  predicate: (token) => token.type === import_types2.AST_TOKEN_TYPES.Punctuator && token.value === ",",
  tokenChar: ","
};
var isLeftCurlyToken = {
  predicate: (token) => token.type === import_types2.AST_TOKEN_TYPES.Punctuator && token.value === "{",
  tokenChar: "{"
};
var isRightCurlyToken = {
  predicate: (token) => token.type === import_types2.AST_TOKEN_TYPES.Punctuator && token.value === "}",
  tokenChar: "}"
};
function assertToken({ predicate, tokenChar }, token) {
  if (token == null) {
    throw new Error(
      `Expected a valid "${tokenChar}" token, but found no token`
    );
  }
  if (!predicate(token)) {
    throw new Error(
      `Expected a valid "${tokenChar}" token, but got "${token.value}" instead`
    );
  }
  return token;
}
var no_unused_vars_default = createRule2({
  name: "no-unused-vars",
  meta: {
    type: "problem",
    docs: {
      description: "Disallow unused variables",
      extendsBaseRule: true,
      recommended: "recommended"
    },
    fixable: "code",
    hasSuggestions: true,
    messages: {
      removeUnusedImportDeclaration: "Remove unused import declaration.",
      removeUnusedVar: 'Remove unused variable "{{varName}}".',
      unusedVar: "'{{varName}}' is {{action}} but never used{{additional}}.",
      usedIgnoredVar: "'{{varName}}' is marked as ignored but is used{{additional}}.",
      usedOnlyAsType: "'{{varName}}' is {{action}} but only used as a type{{additional}}."
    },
    schema: [
      {
        oneOf: [
          {
            type: "string",
            description: "Broad setting for unused variables to target.",
            enum: ["all", "local"]
          },
          {
            type: "object",
            additionalProperties: false,
            properties: {
              args: {
                type: "string",
                description: "Whether to check all, some, or no arguments.",
                enum: ["all", "after-used", "none"]
              },
              argsIgnorePattern: {
                type: "string",
                description: "Regular expressions of argument names to not check for usage."
              },
              caughtErrors: {
                type: "string",
                description: "Whether to check catch block arguments.",
                enum: ["all", "none"]
              },
              caughtErrorsIgnorePattern: {
                type: "string",
                description: "Regular expressions of catch block argument names to not check for usage."
              },
              destructuredArrayIgnorePattern: {
                type: "string",
                description: "Regular expressions of destructured array variable names to not check for usage."
              },
              enableAutofixRemoval: {
                type: "object",
                additionalProperties: false,
                description: "Configurable automatic fixes for different types of unused variables.",
                properties: {
                  imports: {
                    type: "boolean",
                    description: "Whether to enable automatic removal of unused imports."
                  }
                }
              },
              ignoreClassWithStaticInitBlock: {
                type: "boolean",
                description: "Whether to ignore classes with at least one static initialization block."
              },
              ignoreRestSiblings: {
                type: "boolean",
                description: "Whether to ignore sibling properties in `...` destructuring."
              },
              ignoreUsingDeclarations: {
                type: "boolean",
                description: "Whether to ignore using or await using declarations."
              },
              reportUsedIgnorePattern: {
                type: "boolean",
                description: "Whether to report variables that match any of the valid ignore pattern options if they have been used."
              },
              vars: {
                type: "string",
                description: "Whether to check all variables or only locally-declared variables.",
                enum: ["all", "local"]
              },
              varsIgnorePattern: {
                type: "string",
                description: "Regular expressions of variable names to not check for usage."
              }
            }
          }
        ]
      }
    ]
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [{}],
  create(context, [firstOption]) {
    const MODULE_DECL_CACHE = /* @__PURE__ */ new Map();
    const reportedUnusedVariables = /* @__PURE__ */ new Set();
    function areAllSpecifiersUnused(decl) {
      return context.sourceCode.getDeclaredVariables(decl).every((variable) => {
        return reportedUnusedVariables.has(variable);
      });
    }
    const report = (unusedVar, opts) => {
      reportedUnusedVariables.add(unusedVar);
      const writeReferences = unusedVar.references.filter(
        (ref) => ref.isWrite() && ref.from.variableScope === unusedVar.scope.variableScope
      );
      const id = writeReferences.length ? writeReferences[writeReferences.length - 1].identifier : unusedVar.identifiers[0];
      const { start } = id.loc;
      const idLength = id.name.length;
      const loc = {
        start,
        end: {
          column: start.column + idLength,
          line: start.line
        }
      };
      const fixer = (() => {
        const { messageId, fix, useAutofix } = (() => {
          if (unusedVar.defs.length !== 1) {
            return {};
          }
          const { type, def } = defToVariableType(unusedVar.defs[0]);
          switch (type) {
            case 0 /* ArrayDestructure */:
              return {};
            case 1 /* CatchClause */:
              return {};
            case 2 /* ClassName */:
              return {};
            case 3 /* FunctionName */:
              return {};
            case 4 /* ImportBinding */:
              return {
                ...getImportFixer(def),
                useAutofix: options.enableAutofixRemoval.imports
              };
            case 5 /* ImplicitGlobalVariable */:
              return {};
            case 6 /* Parameter */:
              return {};
            case 7 /* TSEnumMember */:
              return {};
            case 8 /* TSEnumName */:
              return {};
            case 9 /* TSModuleName */:
              return {};
            case 10 /* Type */:
              return {};
            case 11 /* Variable */:
              return {};
          }
        })();
        if (!fix) {
          return {};
        }
        if (useAutofix) {
          return { fix };
        }
        const data = {
          varName: unusedVar.name
        };
        return {
          suggest: [
            {
              messageId: messageId ?? "removeUnusedVar",
              data,
              fix
            }
          ]
        };
      })();
      context.report({
        ...opts,
        ...fixer,
        loc,
        node: id
      });
    };
    const options = (() => {
      const options2 = {
        args: "after-used",
        caughtErrors: "all",
        enableAutofixRemoval: {
          imports: false
        },
        ignoreClassWithStaticInitBlock: false,
        ignoreRestSiblings: false,
        ignoreUsingDeclarations: false,
        reportUsedIgnorePattern: false,
        vars: "all"
      };
      if (typeof firstOption === "string") {
        options2.vars = firstOption;
      } else {
        options2.vars = firstOption.vars ?? options2.vars;
        options2.args = firstOption.args ?? options2.args;
        options2.ignoreRestSiblings = firstOption.ignoreRestSiblings ?? options2.ignoreRestSiblings;
        options2.ignoreUsingDeclarations = firstOption.ignoreUsingDeclarations ?? options2.ignoreUsingDeclarations;
        options2.caughtErrors = firstOption.caughtErrors ?? options2.caughtErrors;
        options2.ignoreClassWithStaticInitBlock = firstOption.ignoreClassWithStaticInitBlock ?? options2.ignoreClassWithStaticInitBlock;
        options2.reportUsedIgnorePattern = firstOption.reportUsedIgnorePattern ?? options2.reportUsedIgnorePattern;
        if (firstOption.varsIgnorePattern) {
          options2.varsIgnorePattern = new RegExp(
            firstOption.varsIgnorePattern,
            "u"
          );
        }
        if (firstOption.argsIgnorePattern) {
          options2.argsIgnorePattern = new RegExp(
            firstOption.argsIgnorePattern,
            "u"
          );
        }
        if (firstOption.caughtErrorsIgnorePattern) {
          options2.caughtErrorsIgnorePattern = new RegExp(
            firstOption.caughtErrorsIgnorePattern,
            "u"
          );
        }
        if (firstOption.destructuredArrayIgnorePattern) {
          options2.destructuredArrayIgnorePattern = new RegExp(
            firstOption.destructuredArrayIgnorePattern,
            "u"
          );
        }
        if (firstOption.enableAutofixRemoval) {
          if (firstOption.enableAutofixRemoval.imports != null) {
            options2.enableAutofixRemoval.imports = firstOption.enableAutofixRemoval.imports;
          }
        }
      }
      return options2;
    })();
    function removeNodeWithTrailingNewline(fixer, node) {
      const sourceCode = context.sourceCode;
      const { line: startLine } = node.loc.start;
      const { line: endLine } = node.loc.end;
      const lineRangeStart = sourceCode.getIndexFromLoc({
        column: 0,
        line: startLine
      });
      const lineRangeEnd = endLine < sourceCode.lines.length ? sourceCode.getIndexFromLoc({ column: 0, line: endLine + 1 }) : sourceCode.text.length;
      if (sourceCode.getText(node) === sourceCode.text.slice(lineRangeStart, lineRangeEnd).trim()) {
        return fixer.removeRange([lineRangeStart, lineRangeEnd]);
      }
      return fixer.remove(node);
    }
    function getImportFixer(def) {
      switch (def.node.type) {
        case import_types2.AST_NODE_TYPES.TSImportEqualsDeclaration:
          return {
            messageId: "removeUnusedImportDeclaration",
            fix: (fixer) => removeNodeWithTrailingNewline(fixer, def.node)
          };
        case import_types2.AST_NODE_TYPES.ImportDefaultSpecifier: {
          const importDecl = def.node.parent;
          if (importDecl.specifiers.length === 1 || areAllSpecifiersUnused(importDecl)) {
            return {
              messageId: "removeUnusedImportDeclaration",
              fix: (fixer) => removeNodeWithTrailingNewline(fixer, importDecl)
            };
          }
          return {
            messageId: "removeUnusedVar",
            fix: (fixer) => {
              const comma = nullThrows(
                context.sourceCode.getTokenAfter(def.node),
                NullThrowsReasons.MissingToken(",", "import specifier")
              );
              assertToken(isCommaToken3, comma);
              return fixer.removeRange([
                Math.min(def.node.range[0], comma.range[0]),
                Math.max(def.node.range[1], comma.range[1])
              ]);
            }
          };
        }
        case import_types2.AST_NODE_TYPES.ImportSpecifier: {
          const importDecl = def.node.parent;
          if (importDecl.specifiers.length === 1 || areAllSpecifiersUnused(importDecl)) {
            return {
              messageId: "removeUnusedImportDeclaration",
              fix: (fixer) => removeNodeWithTrailingNewline(fixer, importDecl)
            };
          }
          return {
            messageId: "removeUnusedVar",
            fix: (fixer) => {
              const usedNamedSpecifiers = context.sourceCode.getDeclaredVariables(importDecl).map((variable) => {
                if (reportedUnusedVariables.has(variable)) {
                  return null;
                }
                const specifier = variable.defs[0].node;
                if (specifier.type !== import_types2.AST_NODE_TYPES.ImportSpecifier) {
                  return null;
                }
                return specifier;
              }).filter((v) => v != null);
              if (usedNamedSpecifiers.length === 0) {
                const leftCurly = assertToken(
                  isLeftCurlyToken,
                  context.sourceCode.getFirstToken(
                    importDecl,
                    isLeftCurlyToken.predicate
                  )
                );
                const leftToken = assertToken(
                  isCommaToken3,
                  context.sourceCode.getTokenBefore(leftCurly)
                );
                const rightToken = assertToken(
                  isRightCurlyToken,
                  context.sourceCode.getFirstToken(
                    importDecl,
                    isRightCurlyToken.predicate
                  )
                );
                return fixer.removeRange([
                  leftToken.range[0],
                  rightToken.range[1]
                ]);
              }
              const maybeComma = context.sourceCode.getTokenBefore(def.node);
              const comma = maybeComma && isCommaToken3.predicate(maybeComma) ? maybeComma : assertToken(
                isCommaToken3,
                context.sourceCode.getTokenAfter(def.node)
              );
              return fixer.removeRange([
                Math.min(def.node.range[0], comma.range[0]),
                Math.max(def.node.range[1], comma.range[1])
              ]);
            }
          };
        }
        case import_types2.AST_NODE_TYPES.ImportNamespaceSpecifier: {
          const importDecl = def.node.parent;
          return {
            messageId: "removeUnusedImportDeclaration",
            fix: (fixer) => removeNodeWithTrailingNewline(fixer, importDecl)
          };
        }
      }
    }
    function defToVariableType(def) {
      if (options.destructuredArrayIgnorePattern && def.name.parent.type === import_types2.AST_NODE_TYPES.ArrayPattern) {
        return { type: 0 /* ArrayDestructure */, def };
      }
      switch (def.type) {
        case import_scope_manager4.DefinitionType.CatchClause:
          return { type: 1 /* CatchClause */, def };
        case import_scope_manager4.DefinitionType.ClassName:
          return { type: 2 /* ClassName */, def };
        case import_scope_manager4.DefinitionType.FunctionName:
          return { type: 3 /* FunctionName */, def };
        case import_scope_manager4.DefinitionType.ImplicitGlobalVariable:
          return { type: 5 /* ImplicitGlobalVariable */, def };
        case import_scope_manager4.DefinitionType.ImportBinding:
          return { type: 4 /* ImportBinding */, def };
        case import_scope_manager4.DefinitionType.Parameter:
          return { type: 6 /* Parameter */, def };
        case import_scope_manager4.DefinitionType.TSEnumName:
          return { type: 8 /* TSEnumName */, def };
        case import_scope_manager4.DefinitionType.TSEnumMember:
          return { type: 7 /* TSEnumMember */, def };
        case import_scope_manager4.DefinitionType.TSModuleName:
          return { type: 9 /* TSModuleName */, def };
        case import_scope_manager4.DefinitionType.Type:
          return { type: 10 /* Type */, def };
        case import_scope_manager4.DefinitionType.Variable:
          return { type: 11 /* Variable */, def };
      }
    }
    function getVariableDescription(variableType) {
      switch (variableType) {
        case 0 /* ArrayDestructure */:
          return {
            pattern: options.destructuredArrayIgnorePattern?.toString(),
            variableDescription: "elements of array destructuring"
          };
        case 1 /* CatchClause */:
          return {
            pattern: options.caughtErrorsIgnorePattern?.toString(),
            variableDescription: "caught errors"
          };
        case 6 /* Parameter */:
          return {
            pattern: options.argsIgnorePattern?.toString(),
            variableDescription: "args"
          };
        default:
          return {
            pattern: options.varsIgnorePattern?.toString(),
            variableDescription: "vars"
          };
      }
    }
    function getDefinedMessageData(unusedVar) {
      const def = unusedVar.defs.at(0);
      let additionalMessageData = "";
      if (def) {
        const { pattern, variableDescription } = getVariableDescription(
          defToVariableType(def).type
        );
        if (pattern && variableDescription) {
          additionalMessageData = `. Allowed unused ${variableDescription} must match ${pattern}`;
        }
      }
      return {
        action: "defined",
        additional: additionalMessageData,
        varName: unusedVar.name
      };
    }
    function getAssignedMessageData(unusedVar) {
      const def = unusedVar.defs.at(0);
      let additionalMessageData = "";
      if (def) {
        const { pattern, variableDescription } = getVariableDescription(
          defToVariableType(def).type
        );
        if (pattern && variableDescription) {
          additionalMessageData = `. Allowed unused ${variableDescription} must match ${pattern}`;
        }
      }
      return {
        action: "assigned a value",
        additional: additionalMessageData,
        varName: unusedVar.name
      };
    }
    function getUsedIgnoredMessageData(variable, variableType) {
      const { pattern, variableDescription } = getVariableDescription(variableType);
      let additionalMessageData = "";
      if (pattern && variableDescription) {
        additionalMessageData = `. Used ${variableDescription} must not match ${pattern}`;
      }
      return {
        additional: additionalMessageData,
        varName: variable.name
      };
    }
    function collectUnusedVariables() {
      function hasRestSibling(node) {
        return node.type === import_types2.AST_NODE_TYPES.Property && node.parent.type === import_types2.AST_NODE_TYPES.ObjectPattern && node.parent.properties[node.parent.properties.length - 1].type === import_types2.AST_NODE_TYPES.RestElement;
      }
      function hasRestSpreadSibling(variable) {
        if (options.ignoreRestSiblings) {
          const hasRestSiblingDefinition = variable.defs.some(
            (def) => hasRestSibling(def.name.parent)
          );
          const hasRestSiblingReference = variable.references.some(
            (ref) => hasRestSibling(ref.identifier.parent)
          );
          return hasRestSiblingDefinition || hasRestSiblingReference;
        }
        return false;
      }
      function isAfterLastUsedArg(variable) {
        const def = variable.defs[0];
        const params = context.sourceCode.getDeclaredVariables(def.node);
        const posteriorParams = params.slice(params.indexOf(variable) + 1);
        return !posteriorParams.some(
          (v) => v.eslintUsed || v.references.some((ref) => !isTypeOnlyReference(v, ref))
        );
      }
      const analysisResults = collectVariables(context);
      const variables = [
        ...Array.from(analysisResults.unusedVariables, (variable) => ({
          used: false,
          variable
        })),
        ...Array.from(analysisResults.usedVariables, (variable) => ({
          used: true,
          variable
        }))
      ];
      const unusedVariablesReturn = [];
      for (const { used, variable } of variables) {
        if (variable.defs.length === 0) {
          if (!used) {
            unusedVariablesReturn.push(variable);
          }
          continue;
        }
        const def = variable.defs[0];
        if (variable.scope.type === ts_eslint_exports.Scope.ScopeType.global && options.vars === "local") {
          continue;
        }
        const refUsedInArrayPatterns = variable.references.some(
          (ref) => ref.identifier.parent.type === import_types2.AST_NODE_TYPES.ArrayPattern
        );
        if ((def.name.parent.type === import_types2.AST_NODE_TYPES.ArrayPattern || refUsedInArrayPatterns) && def.name.type === import_types2.AST_NODE_TYPES.Identifier && options.destructuredArrayIgnorePattern?.test(def.name.name)) {
          if (options.reportUsedIgnorePattern && used) {
            report(variable, {
              messageId: "usedIgnoredVar",
              data: getUsedIgnoredMessageData(
                variable,
                0 /* ArrayDestructure */
              )
            });
          }
          continue;
        }
        if (def.type === ts_eslint_exports.Scope.DefinitionType.ClassName) {
          const hasStaticBlock = def.node.body.body.some(
            (node) => node.type === import_types2.AST_NODE_TYPES.StaticBlock
          );
          if (options.ignoreClassWithStaticInitBlock && hasStaticBlock) {
            continue;
          }
        }
        if (def.type === ts_eslint_exports.Scope.DefinitionType.CatchClause) {
          if (options.caughtErrors === "none") {
            continue;
          }
          if (options.caughtErrorsIgnorePattern?.test(def.name.name)) {
            if (options.reportUsedIgnorePattern && used) {
              report(variable, {
                messageId: "usedIgnoredVar",
                data: getUsedIgnoredMessageData(
                  variable,
                  1 /* CatchClause */
                )
              });
            }
            continue;
          }
        } else if (def.type === ts_eslint_exports.Scope.DefinitionType.Parameter) {
          if (options.args === "none") {
            continue;
          }
          if (def.name.type === import_types2.AST_NODE_TYPES.Identifier && options.argsIgnorePattern?.test(def.name.name)) {
            if (options.reportUsedIgnorePattern && used) {
              report(variable, {
                messageId: "usedIgnoredVar",
                data: getUsedIgnoredMessageData(
                  variable,
                  6 /* Parameter */
                )
              });
            }
            continue;
          }
          if (options.args === "after-used" && isFunction(def.name.parent) && !isAfterLastUsedArg(variable)) {
            continue;
          }
        } else if (def.name.type === import_types2.AST_NODE_TYPES.Identifier && options.varsIgnorePattern?.test(def.name.name)) {
          if (options.reportUsedIgnorePattern && used && /* enum members are always marked as 'used' by `collectVariables`, but in reality they may be used or
             unused. either way, don't complain about their naming. */
          def.type !== ts_eslint_exports.Scope.DefinitionType.TSEnumMember) {
            report(variable, {
              messageId: "usedIgnoredVar",
              data: getUsedIgnoredMessageData(variable, 11 /* Variable */)
            });
          }
          continue;
        }
        if (def.type === ts_eslint_exports.Scope.DefinitionType.Variable && options.ignoreUsingDeclarations && (def.parent.kind === "await using" || def.parent.kind === "using")) {
          continue;
        }
        if (hasRestSpreadSibling(variable)) {
          continue;
        }
        if (variable.eslintUsed) {
          continue;
        }
        if (!used) {
          unusedVariablesReturn.push(variable);
        }
      }
      return unusedVariablesReturn;
    }
    return {
      // top-level declaration file handling
      [ambientDeclarationSelector(import_types2.AST_NODE_TYPES.Program)](node) {
        if (!isDefinitionFile(context.filename)) {
          return;
        }
        const moduleDecl = nullThrows(
          node.parent,
          NullThrowsReasons.MissingParent
        );
        if (checkForOverridingExportStatements(moduleDecl)) {
          return;
        }
        markDeclarationChildAsUsed(node);
      },
      // children of a namespace that is a child of a declared namespace are auto-exported
      [ambientDeclarationSelector(
        "TSModuleDeclaration[declare = true] > TSModuleBlock TSModuleDeclaration > TSModuleBlock"
      )](node) {
        const moduleDecl = nullThrows(
          node.parent.parent,
          NullThrowsReasons.MissingParent
        );
        if (checkForOverridingExportStatements(moduleDecl)) {
          return;
        }
        markDeclarationChildAsUsed(node);
      },
      // declared namespace handling
      [ambientDeclarationSelector(
        "TSModuleDeclaration[declare = true] > TSModuleBlock"
      )](node) {
        const moduleDecl = nullThrows(
          node.parent.parent,
          NullThrowsReasons.MissingParent
        );
        if (checkForOverridingExportStatements(moduleDecl)) {
          return;
        }
        markDeclarationChildAsUsed(node);
      },
      // namespace handling in definition files
      [ambientDeclarationSelector("TSModuleDeclaration > TSModuleBlock")](node) {
        if (!isDefinitionFile(context.filename)) {
          return;
        }
        const moduleDecl = nullThrows(
          node.parent.parent,
          NullThrowsReasons.MissingParent
        );
        if (checkForOverridingExportStatements(moduleDecl)) {
          return;
        }
        markDeclarationChildAsUsed(node);
      },
      // collect
      "Program:exit"(programNode) {
        const unusedVars = collectUnusedVariables();
        for (const unusedVar of unusedVars) {
          if (unusedVar.defs.length > 0) {
            const usedOnlyAsType = unusedVar.references.some(
              (ref) => isTypeOnlyReference(unusedVar, ref)
            );
            const messageId = usedOnlyAsType ? "usedOnlyAsType" : "unusedVar";
            const isImportUsedOnlyAsType = usedOnlyAsType && unusedVar.defs.some(
              (def) => def.type === import_scope_manager4.DefinitionType.ImportBinding
            );
            if (isImportUsedOnlyAsType) {
              continue;
            }
            report(unusedVar, {
              messageId,
              data: unusedVar.references.some((ref) => ref.isWrite()) ? getAssignedMessageData(unusedVar) : getDefinedMessageData(unusedVar)
            });
          } else if ("eslintExplicitGlobalComments" in unusedVar && unusedVar.eslintExplicitGlobalComments) {
            const directiveComment = unusedVar.eslintExplicitGlobalComments[0];
            context.report({
              loc: getNameLocationInGlobalDirectiveComment(
                context.sourceCode,
                directiveComment,
                unusedVar.name
              ),
              node: programNode,
              messageId: "unusedVar",
              data: getDefinedMessageData(unusedVar)
            });
          }
        }
      }
    };
    function checkForOverridingExportStatements(node) {
      const cached = MODULE_DECL_CACHE.get(node);
      if (cached != null) {
        return cached;
      }
      const body = getStatementsOfNode(node);
      if (hasOverridingExportStatement(body)) {
        MODULE_DECL_CACHE.set(node, true);
        return true;
      }
      MODULE_DECL_CACHE.set(node, false);
      return false;
    }
    function ambientDeclarationSelector(parent) {
      return [
        // Types are ambiently exported
        `${parent} > :matches(${[
          import_types2.AST_NODE_TYPES.TSInterfaceDeclaration,
          import_types2.AST_NODE_TYPES.TSTypeAliasDeclaration
        ].join(", ")})`,
        // Value things are ambiently exported if they are "declare"d
        `${parent} > :matches(${[
          import_types2.AST_NODE_TYPES.ClassDeclaration,
          import_types2.AST_NODE_TYPES.TSDeclareFunction,
          import_types2.AST_NODE_TYPES.TSEnumDeclaration,
          import_types2.AST_NODE_TYPES.TSModuleDeclaration,
          import_types2.AST_NODE_TYPES.VariableDeclaration
        ].join(", ")})`
      ].join(", ");
    }
    function markDeclarationChildAsUsed(node) {
      const identifiers = [];
      switch (node.type) {
        case import_types2.AST_NODE_TYPES.TSInterfaceDeclaration:
        case import_types2.AST_NODE_TYPES.TSTypeAliasDeclaration:
        case import_types2.AST_NODE_TYPES.ClassDeclaration:
        case import_types2.AST_NODE_TYPES.FunctionDeclaration:
        case import_types2.AST_NODE_TYPES.TSDeclareFunction:
        case import_types2.AST_NODE_TYPES.TSEnumDeclaration:
        case import_types2.AST_NODE_TYPES.TSModuleDeclaration:
          if (node.id?.type === import_types2.AST_NODE_TYPES.Identifier) {
            identifiers.push(node.id);
          }
          break;
        case import_types2.AST_NODE_TYPES.VariableDeclaration:
          for (const declaration of node.declarations) {
            visitPattern(declaration, (pattern) => {
              identifiers.push(pattern);
            });
          }
          break;
      }
      let scope = context.sourceCode.getScope(node);
      const shouldUseUpperScope = [
        import_types2.AST_NODE_TYPES.TSDeclareFunction,
        import_types2.AST_NODE_TYPES.TSModuleDeclaration
      ].includes(node.type);
      if (scope.variableScope !== scope) {
        scope = scope.variableScope;
      } else if (shouldUseUpperScope && scope.upper) {
        scope = scope.upper;
      }
      for (const id of identifiers) {
        const superVar = scope.set.get(id.name);
        if (superVar) {
          superVar.eslintUsed = true;
        }
      }
    }
    function visitPattern(node, cb) {
      const visitor = new import_scope_manager4.PatternVisitor({}, node, cb);
      visitor.visit(node);
    }
  }
});
function hasOverridingExportStatement(body) {
  for (const statement of body) {
    if (statement.type === import_types2.AST_NODE_TYPES.ExportNamedDeclaration && statement.declaration == null || statement.type === import_types2.AST_NODE_TYPES.ExportAllDeclaration || statement.type === import_types2.AST_NODE_TYPES.TSExportAssignment) {
      return true;
    }
    if (statement.type === import_types2.AST_NODE_TYPES.ExportDefaultDeclaration && statement.declaration.type === import_types2.AST_NODE_TYPES.Identifier) {
      return true;
    }
  }
  return false;
}
function getStatementsOfNode(block) {
  if (block.type === import_types2.AST_NODE_TYPES.Program) {
    return block.body;
  }
  return block.body.body;
}

// vendor/packages/eslint-plugin/src/rules/no-var-requires.ts
var no_var_requires_default = createRule2({
  name: "no-var-requires",
  meta: {
    type: "problem",
    deprecated: {
      deprecatedSince: "8.0.0",
      replacedBy: [
        {
          rule: {
            name: "@typescript-eslint/no-require-imports",
            url: "https://typescript-eslint.io/rules/no-require-imports"
          }
        }
      ],
      url: "https://github.com/typescript-eslint/typescript-eslint/pull/8334"
    },
    docs: {
      description: "Disallow `require` statements except in import statements"
    },
    messages: {
      noVarReqs: "Require statement not part of import statement."
    },
    // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint < 9.21
    replacedBy: ["@typescript-eslint/no-require-imports"],
    schema: [
      {
        type: "object",
        additionalProperties: false,
        properties: {
          allow: {
            type: "array",
            description: "Patterns of import paths to allow requiring from.",
            items: { type: "string" }
          }
        }
      }
    ]
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [{ allow: [] }],
  create(context, options) {
    const allowPatterns = options[0].allow.map(
      (pattern) => new RegExp(pattern, "u")
    );
    function isImportPathAllowed(importPath) {
      return allowPatterns.some((pattern) => importPath.match(pattern));
    }
    function isStringOrTemplateLiteral(node) {
      return node.type === import_types2.AST_NODE_TYPES.Literal && typeof node.value === "string" || node.type === import_types2.AST_NODE_TYPES.TemplateLiteral;
    }
    return {
      'CallExpression[callee.name="require"]'(node) {
        if (node.arguments[0] && isStringOrTemplateLiteral(node.arguments[0])) {
          const argValue = getStaticStringValue(node.arguments[0]);
          if (typeof argValue === "string" && isImportPathAllowed(argValue)) {
            return;
          }
        }
        const parent = node.parent.type === import_types2.AST_NODE_TYPES.ChainExpression ? node.parent.parent : node.parent;
        if ([
          import_types2.AST_NODE_TYPES.CallExpression,
          import_types2.AST_NODE_TYPES.MemberExpression,
          import_types2.AST_NODE_TYPES.NewExpression,
          import_types2.AST_NODE_TYPES.TSAsExpression,
          import_types2.AST_NODE_TYPES.TSTypeAssertion,
          import_types2.AST_NODE_TYPES.VariableDeclarator
        ].includes(parent.type)) {
          const variable = ast_utils_exports.findVariable(
            context.sourceCode.getScope(node),
            "require"
          );
          if (!variable?.identifiers.length) {
            context.report({
              node,
              messageId: "noVarReqs"
            });
          }
        }
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/prefer-as-const.ts
var prefer_as_const_default = createRule2({
  name: "prefer-as-const",
  meta: {
    type: "suggestion",
    docs: {
      description: "Enforce the use of `as const` over literal type",
      recommended: "recommended"
    },
    fixable: "code",
    hasSuggestions: true,
    messages: {
      preferConstAssertion: "Expected a `const` instead of a literal type assertion.",
      variableConstAssertion: "Expected a `const` assertion instead of a literal type annotation.",
      variableSuggest: "You should use `as const` instead of type annotation."
    },
    schema: []
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [],
  create(context) {
    function compareTypes(valueNode, typeNode, canFix) {
      if (valueNode.type === import_types2.AST_NODE_TYPES.Literal && typeNode.type === import_types2.AST_NODE_TYPES.TSLiteralType && typeNode.literal.type === import_types2.AST_NODE_TYPES.Literal && valueNode.raw === typeNode.literal.raw) {
        if (canFix) {
          context.report({
            node: typeNode,
            messageId: "preferConstAssertion",
            fix: (fixer) => fixer.replaceText(typeNode, "const")
          });
        } else {
          context.report({
            node: typeNode,
            messageId: "variableConstAssertion",
            suggest: [
              {
                messageId: "variableSuggest",
                fix: (fixer) => [
                  fixer.remove(typeNode.parent),
                  fixer.insertTextAfter(valueNode, " as const")
                ]
              }
            ]
          });
        }
      }
    }
    return {
      PropertyDefinition(node) {
        if (node.value && node.typeAnnotation) {
          compareTypes(node.value, node.typeAnnotation.typeAnnotation, false);
        }
      },
      TSAsExpression(node) {
        compareTypes(node.expression, node.typeAnnotation, true);
      },
      TSTypeAssertion(node) {
        compareTypes(node.expression, node.typeAnnotation, true);
      },
      VariableDeclarator(node) {
        if (node.init && node.id.typeAnnotation) {
          compareTypes(node.init, node.id.typeAnnotation.typeAnnotation, false);
        }
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/triple-slash-reference.ts
var triple_slash_reference_default = createRule2({
  name: "triple-slash-reference",
  meta: {
    type: "suggestion",
    docs: {
      description: "Disallow certain triple slash directives in favor of ES6-style import declarations",
      recommended: "recommended"
    },
    messages: {
      tripleSlashReference: "Do not use a triple slash reference for {{module}}, use `import` style instead."
    },
    schema: [
      {
        type: "object",
        additionalProperties: false,
        properties: {
          lib: {
            type: "string",
            description: 'What to enforce for `/// <reference lib="..." />` references.',
            enum: ["always", "never"]
          },
          path: {
            type: "string",
            description: 'What to enforce for `/// <reference path="..." />` references.',
            enum: ["always", "never"]
          },
          types: {
            type: "string",
            description: 'What to enforce for `/// <reference types="..." />` references.',
            enum: ["always", "never", "prefer-import"]
          }
        }
      }
    ]
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [
    {
      lib: "always",
      path: "never",
      types: "prefer-import"
    }
  ],
  create(context, [{ lib, path, types }]) {
    let programNode;
    const references = [];
    function hasMatchingReference(source) {
      references.forEach((reference) => {
        if (reference.importName === source.value) {
          context.report({
            node: reference.comment,
            messageId: "tripleSlashReference",
            data: {
              module: reference.importName
            }
          });
        }
      });
    }
    return {
      ImportDeclaration(node) {
        if (programNode) {
          hasMatchingReference(node.source);
        }
      },
      Program(node) {
        if (lib === "always" && path === "always" && types === "always") {
          return;
        }
        programNode = node;
        const referenceRegExp = /^\/\s*<reference\s*(types|path|lib)\s*=\s*["|'](.*)["|']/;
        const firstToken = context.sourceCode.getFirstToken(programNode);
        const commentsBefore = firstToken ? context.sourceCode.getCommentsBefore(firstToken) : context.sourceCode.getAllComments();
        commentsBefore.forEach((comment) => {
          if (comment.type !== import_types2.AST_TOKEN_TYPES.Line) {
            return;
          }
          const referenceResult = referenceRegExp.exec(comment.value);
          if (referenceResult) {
            if (referenceResult[1] === "types" && types === "never" || referenceResult[1] === "path" && path === "never" || referenceResult[1] === "lib" && lib === "never") {
              context.report({
                node: comment,
                messageId: "tripleSlashReference",
                data: {
                  module: referenceResult[2]
                }
              });
              return;
            }
            if (referenceResult[1] === "types" && types === "prefer-import") {
              references.push({ comment, importName: referenceResult[2] });
            }
          }
        });
      },
      TSImportEqualsDeclaration(node) {
        if (programNode) {
          const reference = node.moduleReference;
          if (reference.type === import_types2.AST_NODE_TYPES.TSExternalModuleReference) {
            hasMatchingReference(reference.expression);
          }
        }
      }
    };
  }
});

// vendor/packages/eslint-plugin/src/rules/no-explicit-any.ts
var no_explicit_any_default = createRule2({
  name: "no-explicit-any",
  meta: {
    type: "suggestion",
    docs: {
      description: "Disallow the `any` type",
      recommended: "recommended"
    },
    fixable: "code",
    hasSuggestions: true,
    messages: {
      suggestNever: "Use `never` instead, this is useful when instantiating generic type parameters that you don't need to know the type of.",
      suggestPropertyKey: "Use `PropertyKey` instead, this is more explicit than `keyof any`.",
      suggestUnknown: "Use `unknown` instead, this will force you to explicitly, and safely assert the type is correct.",
      unexpectedAny: "Unexpected any. Specify a different type."
    },
    schema: [
      {
        type: "object",
        additionalProperties: false,
        properties: {
          fixToUnknown: {
            type: "boolean",
            description: "Whether to enable auto-fixing in which the `any` type is converted to the `unknown` type."
          },
          ignoreRestArgs: {
            type: "boolean",
            description: "Whether to ignore rest parameter arrays."
          }
        }
      }
    ]
  },
  // eslint-disable-next-line @typescript-eslint/no-deprecated -- For compatibility with ESLint 8, see #12842
  defaultOptions: [
    {
      fixToUnknown: false,
      ignoreRestArgs: false
    }
  ],
  create(context, [{ fixToUnknown, ignoreRestArgs }]) {
    function isNodeValidFunction(node) {
      return [
        import_types2.AST_NODE_TYPES.ArrowFunctionExpression,
        // const x = (...args: any[]) => {};
        import_types2.AST_NODE_TYPES.FunctionDeclaration,
        // function f(...args: any[]) {}
        import_types2.AST_NODE_TYPES.FunctionExpression,
        // const x = function(...args: any[]) {};
        import_types2.AST_NODE_TYPES.TSCallSignatureDeclaration,
        // type T = {(...args: any[]): unknown};
        import_types2.AST_NODE_TYPES.TSConstructorType,
        // type T = new (...args: any[]) => unknown
        import_types2.AST_NODE_TYPES.TSConstructSignatureDeclaration,
        // type T = {new (...args: any[]): unknown};
        import_types2.AST_NODE_TYPES.TSDeclareFunction,
        // declare function _8(...args: any[]): unknown;
        import_types2.AST_NODE_TYPES.TSEmptyBodyFunctionExpression,
        // declare class A { f(...args: any[]): unknown; }
        import_types2.AST_NODE_TYPES.TSFunctionType,
        // type T = (...args: any[]) => unknown;
        import_types2.AST_NODE_TYPES.TSMethodSignature
        // type T = {f(...args: any[]): unknown};
      ].includes(node.type);
    }
    function isNodeRestElementInFunction(node) {
      return node.type === import_types2.AST_NODE_TYPES.RestElement && isNodeValidFunction(node.parent);
    }
    function isNodeReadonlyTSTypeOperator(node) {
      return node.type === import_types2.AST_NODE_TYPES.TSTypeOperator && node.operator === "readonly";
    }
    function isNodeValidArrayTSTypeReference(node) {
      return node.type === import_types2.AST_NODE_TYPES.TSTypeReference && node.typeName.type === import_types2.AST_NODE_TYPES.Identifier && ["Array", "ReadonlyArray"].includes(node.typeName.name);
    }
    function isNodeValidTSType(node) {
      return isNodeReadonlyTSTypeOperator(node) || isNodeValidArrayTSTypeReference(node);
    }
    function isGreatGrandparentRestElement(node) {
      return node.parent?.parent?.parent != null && isNodeRestElementInFunction(node.parent.parent.parent);
    }
    function isGreatGreatGrandparentRestElement(node) {
      return node.parent?.parent?.parent?.parent != null && isNodeValidTSType(node.parent.parent) && isNodeRestElementInFunction(node.parent.parent.parent.parent);
    }
    function isNodeDescendantOfRestElementInFunction(node) {
      return isGreatGrandparentRestElement(node) || isGreatGreatGrandparentRestElement(node);
    }
    function isNodeWithinKeyofAny(node) {
      return node.parent.type === import_types2.AST_NODE_TYPES.TSTypeOperator && node.parent.operator === "keyof";
    }
    function createPropertyKeyFixer(node) {
      return (fixer) => {
        return fixer.replaceText(node.parent, "PropertyKey");
      };
    }
    function createUnknownFixer(node) {
      return (fixer) => {
        return fixer.replaceText(
          node,
          node.parent.type === import_types2.AST_NODE_TYPES.TSTypeAnnotation && isNodeRestElementInFunction(node.parent.parent) ? "unknown[]" : "unknown"
        );
      };
    }
    return {
      TSAnyKeyword(node) {
        const isKeyofAny = isNodeWithinKeyofAny(node);
        if (ignoreRestArgs && isNodeDescendantOfRestElementInFunction(node)) {
          return;
        }
        const fixOrSuggest = {
          fix: null,
          suggest: isKeyofAny ? [
            {
              messageId: "suggestPropertyKey",
              fix: createPropertyKeyFixer(node)
            }
          ] : [
            {
              messageId: "suggestUnknown",
              fix: createUnknownFixer(node)
            },
            {
              messageId: "suggestNever",
              fix: (fixer) => fixer.replaceText(node, "never")
            }
          ]
        };
        if (fixToUnknown) {
          fixOrSuggest.fix = isKeyofAny ? createPropertyKeyFixer(node) : createUnknownFixer(node);
        }
        context.report({
          node,
          messageId: "unexpectedAny",
          ...fixOrSuggest
        });
      }
    };
  }
});

// generated-rule-entry.ts
var rules = {
  "no-array-constructor": no_array_constructor_default,
  "no-duplicate-enum-values": no_duplicate_enum_values_default,
  "no-extra-non-null-assertion": no_extra_non_null_assertion_default,
  "no-loss-of-precision": no_loss_of_precision_default,
  "no-misused-new": no_misused_new_default,
  "no-namespace": no_namespace_default,
  "no-non-null-asserted-optional-chain": no_non_null_asserted_optional_chain_default,
  "no-this-alias": no_this_alias_default,
  "no-unnecessary-type-constraint": no_unnecessary_type_constraint_default,
  "no-unsafe-declaration-merging": no_unsafe_declaration_merging_default,
  "no-unused-vars": no_unused_vars_default,
  "no-var-requires": no_var_requires_default,
  "prefer-as-const": prefer_as_const_default,
  "triple-slash-reference": triple_slash_reference_default,
  "no-explicit-any": no_explicit_any_default
};
var generated_rule_entry_default = { meta: { name: "@agent-teams/eslint-native-syntax", version: "0.1.0" }, rules };
// Annotate the CommonJS export names for ESM import in node:
0 && (module.exports = {
  rules
});
